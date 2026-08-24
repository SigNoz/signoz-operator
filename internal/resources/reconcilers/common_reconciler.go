package reconcilers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	resourcesv1alpha1 "github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/clients"
	"github.com/SigNoz/signoz-operator/internal/providerconfig"
	"github.com/SigNoz/signoz-operator/internal/resources"
)

type engine struct {
	client               client.Client
	resolver             providerconfig.Resolver
	adapter              resources.Adapter
	defaultInterval      time.Duration
	defaultRetryInterval time.Duration
	defaultTimeout       time.Duration
	operatorNamespace    string
}

// operatorNamespace is where a ClusterProviderConfig's Secret and ConfigMap
// references resolve.
func NewCommonReconciler(c client.Client, resolver providerconfig.Resolver, adapter resources.Adapter, interval, retryInterval, timeout time.Duration, operatorNamespace string) resources.Reconciler {
	return &engine{
		client:               c,
		resolver:             resolver,
		adapter:              adapter,
		defaultInterval:      interval,
		defaultRetryInterval: retryInterval,
		defaultTimeout:       timeout,
		operatorNamespace:    operatorNamespace,
	}
}

func (e *engine) Reconcile(ctx context.Context, obj resources.Object) (ctrl.Result, error) {
	if timeout := e.timeout(obj); timeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	if !obj.K8sObject().GetDeletionTimestamp().IsZero() {
		return e.finalize(ctx, obj)
	}

	before := obj.GetCoreStatus().DeepCopy()

	result, err := e.reconcile(ctx, obj)

	if commitErr := e.commit(ctx, obj, before); commitErr != nil && err == nil {
		return ctrl.Result{}, commitErr
	}

	return result, err
}

// reconcile sets conditions on the live status and returns the requeue; the
// caller commits the status once.
func (e *engine) reconcile(ctx context.Context, obj resources.Object) (ctrl.Result, error) {
	k8sObject := obj.K8sObject()

	if !controllerutil.ContainsFinalizer(k8sObject, resourcesv1alpha1.ResourceFinalizer) {
		controllerutil.AddFinalizer(k8sObject, resourcesv1alpha1.ResourceFinalizer)
		if err := e.client.Update(ctx, k8sObject); err != nil {
			return ctrl.Result{}, fmt.Errorf("could not add finalizer: %w", err)
		}

		// The update event requeues; nothing else runs until the finalizer is set.
		return ctrl.Result{}, nil
	}

	spec := obj.GetCoreSpec()
	status := obj.GetCoreStatus()
	generation := k8sObject.GetGeneration()

	if spec.Suspend {
		resources.SetConditions(status, generation, resources.ReconcilerOutcomeSuspended, resources.ReasonSuspended, "Reconciliation is suspended by spec.suspend")

		return ctrl.Result{}, nil
	}

	identity, err := obj.Identity()
	if err != nil {
		resources.SetConditions(status, generation, resources.ReconcilerOutcomeTerminal, resources.ReasonInvalidSpec, "Could not derive identity: "+err.Error())

		return ctrl.Result{}, nil
	}

	hash, err := obj.Hash()
	if err != nil {
		resources.SetConditions(status, generation, resources.ReconcilerOutcomeTerminal, resources.ReasonInvalidSpec, "Could not render objectTemplate: "+err.Error())

		return ctrl.Result{}, nil
	}

	c, err := e.connect(ctx, obj, spec)
	if err != nil {
		resources.SetConditions(status, generation, resources.ReconcilerOutcomeRecoverable, resources.ReasonProviderConfigNotReady, err.Error())

		return ctrl.Result{RequeueAfter: e.retryInterval(obj)}, nil
	}

	var result ctrl.Result

	if currentID(status) == "" {
		result, err = e.establish(ctx, obj, c, identity, hash)
	} else {
		result, err = e.reconcileExisting(ctx, obj, c, hash)
	}

	// Stamp the heartbeat after every adapter call and metadata patch, so a patch
	// re-reading the object cannot drop it. It marks a reconcile that reached
	// SigNoz, so the connect-failure and suspend paths above deliberately omit it.
	status.ReconciledAt = metav1.Now()

	return result, err
}

// establish resolves an object with no recorded id: one identity match is
// adopted, none creates, more than one is ambiguous. A pinned
// signoz-resource-id selects among the matches, never overrides them. See
// docs/idempotency.md.
func (e *engine) establish(
	ctx context.Context,
	obj resources.Object,
	c clients.SigNoz,
	identity, hash string,
) (ctrl.Result, error) {
	status := obj.GetCoreStatus()
	generation := obj.K8sObject().GetGeneration()

	found, err := e.adapter.Find(ctx, c, obj)
	if err != nil {
		return e.fail(obj, resources.AdapterOperationFind, err)
	}

	if pinned := obj.K8sObject().GetAnnotations()[resourcesv1alpha1.AnnotationSigNozResourceID]; pinned != "" {
		for _, resourceMetadata := range found {
			if resourceMetadata.ID != nil && *resourceMetadata.ID == pinned {
				return e.adopt(ctx, obj, c, resourceMetadata, hash)
			}
		}

		resources.SetConditions(status, generation, resources.ReconcilerOutcomeTerminal, resources.ReasonSigNozResourceIDMismatch,
			fmt.Sprintf("annotation %s: no object with id %q matches identity %q%s", resourcesv1alpha1.AnnotationSigNozResourceID, pinned, identity, candidateIDs(found)))

		return ctrl.Result{}, nil
	}

	switch len(found) {
	case 1:
		return e.adopt(ctx, obj, c, found[0], hash)

	case 0:
		return e.create(ctx, obj, c, identity, hash)

	default:
		resources.SetConditions(status, generation, resources.ReconcilerOutcomeTerminal, resources.ReasonAmbiguous,
			fmt.Sprintf("ambiguous: %d objects in SigNoz match identity %q%s; set annotation %s to the id of the one to adopt", len(found), identity, candidateIDs(found), resourcesv1alpha1.AnnotationSigNozResourceID))

		return ctrl.Result{}, nil
	}
}

// candidateIDs renders the ids of the matched objects for a condition message,
// empty when none carries one.
func candidateIDs(found []*resourcesv1alpha1.SigNozResource) string {
	ids := make([]string, 0, len(found))

	for _, resourceMetadata := range found {
		if resourceMetadata.ID != nil {
			ids = append(ids, *resourceMetadata.ID)
		}
	}

	if len(ids) == 0 {
		return ""
	}

	return fmt.Sprintf(" (ids %s)", strings.Join(ids, ", "))
}

// create records the attempt, then POSTs once. The POST is never retried within
// an attempt; an unknown outcome keeps the record and requeues so a later Find
// resolves it. See docs/idempotency.md.
func (e *engine) create(
	ctx context.Context,
	obj resources.Object,
	c clients.SigNoz,
	identity, hash string,
) (ctrl.Result, error) {
	status := obj.GetCoreStatus()
	generation := obj.K8sObject().GetGeneration()

	if err := e.recordCreateAttempt(ctx, obj); err != nil {
		return ctrl.Result{}, fmt.Errorf("could not record create attempt: %w", err)
	}

	resourceMetadata, err := e.adapter.Create(ctx, c, obj)
	if err != nil {
		return e.afterFailedCreate(ctx, obj, c, identity, hash, err)
	}

	// Clear the record before touching status: a metadata patch re-reads the
	// object and would drop unsaved status changes.
	if err := e.clearCreateAttempt(ctx, obj); err != nil {
		return ctrl.Result{}, err
	}

	status.SigNozResource = resourceMetadata
	status.ObservedHash = hash
	resources.SetConditions(status, generation, resources.ReconcilerOutcomeSynced, resources.ReasonCreated, "Created in SigNoz")

	return ctrl.Result{RequeueAfter: e.interval(obj)}, nil
}

func (e *engine) afterFailedCreate(
	ctx context.Context,
	obj resources.Object,
	c clients.SigNoz,
	identity, hash string,
	err error,
) (ctrl.Result, error) {
	status := obj.GetCoreStatus()
	generation := obj.K8sObject().GetGeneration()

	var apiErr *resources.AdapterError
	if !errors.As(err, &apiErr) {
		// A transport error leaves the outcome unknown: keep the record.
		resources.SetConditions(status, generation, resources.ReconcilerOutcomeRecoverable, resources.ReasonBackendUnreachable, "create: could not reach SigNoz: "+err.Error())

		return ctrl.Result{RequeueAfter: e.retryInterval(obj)}, nil
	}

	switch {
	case apiErr.HTTPStatusCode == http.StatusConflict:
		return e.resolveConflict(ctx, obj, c, identity, hash)

	case apiErr.Outcome == resources.AdapterOutcomeTerminal:
		if rmErr := e.clearCreateAttempt(ctx, obj); rmErr != nil {
			return ctrl.Result{}, rmErr
		}

		resources.SetConditions(status, generation, resources.ReconcilerOutcomeTerminal, terminalReason(apiErr), apiErr.Message)

		return ctrl.Result{}, nil

	default:
		// A 5xx leaves the outcome unknown: keep the record.
		resources.SetConditions(status, generation, resources.ReconcilerOutcomeRecoverable, resources.ReasonBackendError, apiErr.Message)

		return ctrl.Result{RequeueAfter: e.retryInterval(obj)}, nil
	}
}

// resolveConflict resolves a 409 on create. A conflict with no matching object
// is reported rather than retried.
func (e *engine) resolveConflict(
	ctx context.Context,
	obj resources.Object,
	c clients.SigNoz,
	identity, hash string,
) (ctrl.Result, error) {
	status := obj.GetCoreStatus()
	generation := obj.K8sObject().GetGeneration()

	found, err := e.adapter.Find(ctx, c, obj)
	if err != nil {
		return e.fail(obj, resources.AdapterOperationFind, err)
	}

	switch len(found) {
	case 1:
		return e.adopt(ctx, obj, c, found[0], hash)

	case 0:
		if rmErr := e.clearCreateAttempt(ctx, obj); rmErr != nil {
			return ctrl.Result{}, rmErr
		}

		resources.SetConditions(status, generation, resources.ReconcilerOutcomeTerminal, resources.ReasonRejected,
			fmt.Sprintf("SigNoz reported a conflict for identity %q but no matching object was found", identity))

		return ctrl.Result{}, nil

	default:
		resources.SetConditions(status, generation, resources.ReconcilerOutcomeTerminal, resources.ReasonAmbiguous,
			fmt.Sprintf("ambiguous: %d objects in SigNoz match identity %q%s; set annotation %s to the id of the one to adopt", len(found), identity, candidateIDs(found), resourcesv1alpha1.AnnotationSigNozResourceID))

		return ctrl.Result{}, nil
	}
}

// adopt takes ownership of an existing object: it may not be one the operator
// wrote, so it is compared and updated rather than assumed to match.
func (e *engine) adopt(
	ctx context.Context,
	obj resources.Object,
	c clients.SigNoz,
	resourceMetadata *resourcesv1alpha1.SigNozResource,
	hash string,
) (ctrl.Result, error) {
	// Clear the record before touching status: a metadata patch re-reads the
	// object and would drop unsaved status changes.
	if err := e.clearCreateAttempt(ctx, obj); err != nil {
		return ctrl.Result{}, err
	}

	obj.GetCoreStatus().SigNozResource = resourceMetadata

	return e.reconcileExisting(ctx, obj, c, hash)
}

func (e *engine) reconcileExisting(
	ctx context.Context,
	obj resources.Object,
	c clients.SigNoz,
	hash string,
) (ctrl.Result, error) {
	status := obj.GetCoreStatus()
	generation := obj.K8sObject().GetGeneration()
	resourceMetadata := status.SigNozResource

	found, remote, err := e.adapter.Read(ctx, c, resourceMetadata)
	if err != nil {
		return e.fail(obj, resources.AdapterOperationRead, err)
	}

	if !found {
		// The remote is gone. Drop the stale metadata; the create-attempt record
		// is already cleared, so the next pass finds nothing and creates afresh
		// with no risk of a duplicate.
		status.SigNozResource = nil
		status.ObservedHash = ""
		resources.SetConditions(status, generation, resources.ReconcilerOutcomePending, resources.ReasonPending, "The SigNoz object was not found; it will be recreated")

		return ctrl.Result{RequeueAfter: e.retryInterval(obj)}, nil
	}

	drift, err := obj.Compare(remote)
	if err != nil {
		resources.SetConditions(status, generation, resources.ReconcilerOutcomeRecoverable, resources.ReasonBackendError, "read: could not compare against the remote object: "+err.Error())

		return ctrl.Result{RequeueAfter: e.retryInterval(obj)}, nil
	}

	if len(drift.ImmutableFields) > 0 {
		resources.SetConditions(status, generation, resources.ReconcilerOutcomeTerminal, resources.ReasonImmutableFieldChanged,
			fmt.Sprintf("Immutable fields changed: %s. Revert the change, or delete and recreate the resource", strings.Join(drift.ImmutableFields, ", ")))

		return ctrl.Result{}, nil
	}

	upToDate := len(drift.UpdatableFields) == 0

	// An empty recorded hash has no prior write to compare against — a first
	// observation or a fresh adoption — so the managed-field compare is trusted
	// alone. It lets a kind whose remote state cannot be compared
	// field-for-field still detect a spec edit, per docs/core-status.md.
	desiredChanged := status.ObservedHash != "" && status.ObservedHash != hash

	if upToDate && !desiredChanged {
		status.ObservedHash = hash
		resources.SetConditions(status, generation, resources.ReconcilerOutcomeSynced, resources.ReasonSynced, "In sync with SigNoz")

		return ctrl.Result{RequeueAfter: e.interval(obj)}, nil
	}

	if err := e.adapter.Update(ctx, c, resourceMetadata, obj); err != nil {
		return e.fail(obj, resources.AdapterOperationUpdate, err)
	}

	status.ObservedHash = hash
	resources.SetConditions(status, generation, resources.ReconcilerOutcomeSynced, resources.ReasonUpdated, "Updated in SigNoz")

	return ctrl.Result{RequeueAfter: e.interval(obj)}, nil
}

// finalize applies the reclaim policy. A Delete policy that cannot reach SigNoz
// requeues rather than orphaning the remote object silently.
func (e *engine) finalize(ctx context.Context, obj resources.Object) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(obj.K8sObject(), resourcesv1alpha1.ResourceFinalizer) {
		return ctrl.Result{}, nil
	}

	spec := obj.GetCoreSpec()

	if spec.ReclaimPolicy == resourcesv1alpha1.ReclaimOrphan {
		return e.removeFinalizer(ctx, obj)
	}

	c, err := e.connect(ctx, obj, spec)
	if err != nil {
		log.Info("Waiting to reclaim the SigNoz object: its provider config is not resolvable")

		return ctrl.Result{RequeueAfter: e.retryInterval(obj)}, nil
	}

	resourceMetadata := obj.GetCoreStatus().SigNozResource
	if currentID(obj.GetCoreStatus()) == "" {
		// An unconfirmed create may have left an object behind. Resolve it by
		// identity, and only drop the finalizer when a lookup positively confirms
		// there is nothing to reclaim — never on a failed or ambiguous lookup,
		// which would orphan the remote object the reclaim policy meant to delete.
		identity, err := obj.Identity()
		if err != nil {
			log.Error(err, "Cannot derive an identity to reclaim the SigNoz object by; removing finalizer")

			return e.removeFinalizer(ctx, obj)
		}

		found, ferr := e.adapter.Find(ctx, c, obj)
		if ferr != nil {
			log.Error(ferr, "Could not look up the SigNoz object to reclaim; will retry")

			return ctrl.Result{RequeueAfter: e.retryInterval(obj)}, nil
		}

		switch len(found) {
		case 0:
			return e.removeFinalizer(ctx, obj)
		case 1:
			resourceMetadata = found[0]
		default:
			log.Info("Ambiguous SigNoz objects match this resource; will not guess which to reclaim", "count", len(found), "identity", identity)

			return ctrl.Result{RequeueAfter: e.retryInterval(obj)}, nil
		}
	}

	if err := e.adapter.Delete(ctx, c, resourceMetadata); err != nil {
		log.Error(err, "Could not reclaim the SigNoz object")

		return ctrl.Result{RequeueAfter: e.retryInterval(obj)}, nil
	}

	log.Info("Reclaimed the SigNoz object")

	return e.removeFinalizer(ctx, obj)
}

func (e *engine) removeFinalizer(ctx context.Context, obj resources.Object) (ctrl.Result, error) {
	k8sObject := obj.K8sObject()

	controllerutil.RemoveFinalizer(k8sObject, resourcesv1alpha1.ResourceFinalizer)

	if err := e.client.Update(ctx, k8sObject); err != nil {
		return ctrl.Result{}, fmt.Errorf("could not remove finalizer: %w", err)
	}

	return ctrl.Result{}, nil
}

// connect turns the resource's provider config ref into a client. Every
// failure is recoverable: a missing or broken config never settles the
// resource.
func (e *engine) connect(ctx context.Context, obj resources.Object, spec *resourcesv1alpha1.CoreSpec) (clients.SigNoz, error) {
	resolved, err := e.resolver.ResolveRef(ctx, spec.ProviderConfigRef, obj.K8sObject().GetNamespace(), e.operatorNamespace)
	if err != nil {
		return nil, err
	}

	return clients.New(resolved), nil
}

func (e *engine) fail(obj resources.Object, op resources.AdapterOperation, err error) (ctrl.Result, error) {
	status := obj.GetCoreStatus()
	generation := obj.K8sObject().GetGeneration()

	var apiErr *resources.AdapterError
	if errors.As(err, &apiErr) {
		if apiErr.Outcome == resources.AdapterOutcomeTerminal {
			resources.SetConditions(status, generation, resources.ReconcilerOutcomeTerminal, terminalReason(apiErr), apiErr.Message)

			return ctrl.Result{}, nil
		}

		resources.SetConditions(status, generation, resources.ReconcilerOutcomeRecoverable, resources.ReasonBackendError, apiErr.Message)

		return ctrl.Result{RequeueAfter: e.retryInterval(obj)}, nil
	}

	resources.SetConditions(status, generation, resources.ReconcilerOutcomeRecoverable, resources.ReasonBackendUnreachable,
		fmt.Sprintf("%s: could not reach SigNoz: %s", op, err))

	return ctrl.Result{RequeueAfter: e.retryInterval(obj)}, nil
}

func terminalReason(apiErr *resources.AdapterError) resources.Reason {
	if apiErr.AttributableToProvider() {
		return resources.ReasonUnauthorized
	}

	return resources.ReasonRejected
}

func (e *engine) commit(ctx context.Context, obj resources.Object, before *resourcesv1alpha1.CoreStatus) error {
	if apiequality.Semantic.DeepEqual(before, obj.GetCoreStatus()) {
		return nil
	}

	if err := e.client.Status().Update(ctx, obj.K8sObject()); err != nil {
		return fmt.Errorf("could not update status: %w", err)
	}

	return nil
}

// A metadata patch re-reads the object, so a caller must not hold unsaved
// status changes across it.
func (e *engine) recordCreateAttempt(ctx context.Context, obj resources.Object) error {
	k8sObject := obj.K8sObject()
	patch := client.MergeFrom(k8sObject.DeepCopyObject().(client.Object))

	annotations := k8sObject.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	annotations[resourcesv1alpha1.AnnotationCreateAttempt] = time.Now().UTC().Format(time.RFC3339)
	k8sObject.SetAnnotations(annotations)

	return e.client.Patch(ctx, k8sObject, patch)
}

func (e *engine) clearCreateAttempt(ctx context.Context, obj resources.Object) error {
	k8sObject := obj.K8sObject()

	if _, ok := k8sObject.GetAnnotations()[resourcesv1alpha1.AnnotationCreateAttempt]; !ok {
		return nil
	}

	patch := client.MergeFrom(k8sObject.DeepCopyObject().(client.Object))

	annotations := k8sObject.GetAnnotations()
	delete(annotations, resourcesv1alpha1.AnnotationCreateAttempt)
	k8sObject.SetAnnotations(annotations)

	return e.client.Patch(ctx, k8sObject, patch)
}

func (e *engine) timeout(obj resources.Object) time.Duration {
	if d := obj.GetCoreSpec().Timeout; d != nil && d.Duration > 0 {
		return d.Duration
	}

	return e.defaultTimeout
}

func (e *engine) interval(obj resources.Object) time.Duration {
	if d := obj.GetCoreSpec().Interval; d != nil && d.Duration > 0 {
		return d.Duration
	}

	return e.defaultInterval
}

func (e *engine) retryInterval(obj resources.Object) time.Duration {
	spec := obj.GetCoreSpec()

	if d := spec.RetryInterval; d != nil && d.Duration > 0 {
		return d.Duration
	}

	if d := spec.Interval; d != nil && d.Duration > 0 {
		return d.Duration
	}

	if e.defaultRetryInterval > 0 {
		return e.defaultRetryInterval
	}

	return e.defaultInterval
}

func currentID(status *resourcesv1alpha1.CoreStatus) string {
	if status.SigNozResource != nil && status.SigNozResource.ID != nil {
		return *status.SigNozResource.ID
	}

	return ""
}
