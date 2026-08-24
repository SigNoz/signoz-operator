package reconcilers

import (
	"context"
	"fmt"
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
	"github.com/SigNoz/signoz-operator/internal/errors"
	"github.com/SigNoz/signoz-operator/internal/providerconfig"
	"github.com/SigNoz/signoz-operator/internal/resources"
)

type commonReconciler struct {
	client                 client.Client
	resolver               providerconfig.Resolver
	adapter                resources.Adapter
	defaultInterval        time.Duration
	defaultRetryInterval   time.Duration
	defaultTimeout         time.Duration
	requeueAfterOnNotFound time.Duration
	operatorNamespace      string
}

func NewCommonReconciler(kubeClient client.Client, resolver providerconfig.Resolver, adapter resources.Adapter, interval, retryInterval, timeout time.Duration, operatorNamespace string) resources.Reconciler {
	return &commonReconciler{
		client:                 kubeClient,
		resolver:               resolver,
		adapter:                adapter,
		defaultInterval:        interval,
		defaultRetryInterval:   retryInterval,
		defaultTimeout:         timeout,
		requeueAfterOnNotFound: 1 * time.Second,
		operatorNamespace:      operatorNamespace,
	}
}

func (reconciler *commonReconciler) Reconcile(ctx context.Context, obj resources.Object) (ctrl.Result, error) {
	logger := logf.FromContext(ctx).WithName("CommonReconciler.Reconcile")

	// Set timeout for the entire reconciler based on obj.timeout or default timeout.
	if timeout := reconciler.timeout(obj); timeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// If the object has been deleted, finalize it.
	if !obj.K8sObject().GetDeletionTimestamp().IsZero() {
		return reconciler.finalize(ctx, obj)
	}

	// Add the finalizer before doing anything else. Fail and stop if finalizer cannot be added.
	if err := reconciler.addFinalizer(ctx, obj); err != nil {
		return ctrl.Result{}, err
	}

	beforeStatus := obj.GetCoreStatus().DeepCopy()
	k8sObject := obj.K8sObject()
	spec := obj.GetCoreSpec()
	status := obj.GetCoreStatus()
	generation := k8sObject.GetGeneration()

	// Nothing to do if the spec has been suspended.
	if spec.Suspend {
		logger.Info("Object has spec.suspend set, there is no nothing to do")
		resources.SetConditionsOnOutcome(
			status,
			generation,
			resources.ReconcilerOutcomeSuspended,
			resources.ReasonSuspended,
			"reconciliation is suspended by spec.suspend",
		)
		return ctrl.Result{}, nil
	}

	identity, err := obj.Identity()
	if err != nil {
		logger.Error(err, "Identity could not be derived for this object, this is likely a malformed input, fix the spec and try again")
		resources.SetConditionsOnOutcome(
			status,
			generation,
			resources.ReconcilerOutcomeTerminal,
			resources.ReasonInvalidSpec,
			"could not derive identity: "+err.Error(),
		)
		return ctrl.Result{}, nil
	}

	hash, err := obj.Hash()
	if err != nil {
		logger.Error(err, "Hash could not be calculated, this is likely a malformed input, fix the spec")
		resources.SetConditionsOnOutcome(
			status,
			generation,
			resources.ReconcilerOutcomeTerminal,
			resources.ReasonInvalidSpec, "could not calculate hash: "+err.Error(),
		)
		return ctrl.Result{}, nil
	}

	sigNozClient, err := reconciler.resolveProviderConfig(ctx, obj, spec)
	if err != nil {
		resources.SetConditionsOnOutcome(
			status,
			generation,
			resources.ReconcilerOutcomeRecoverable,
			resources.ReasonProviderConfigNotReady,
			err.Error(),
		)
		return ctrl.Result{RequeueAfter: reconciler.retryInterval(obj)}, nil
	}

	var result ctrl.Result
	_, err = resourcesv1alpha1.GetIDFromSigNozResource(status.SigNozResource)
	if err != nil {
		// This is a new object.
		result, err = reconciler.OnNewObject(ctx, obj, sigNozClient, identity, hash)
	} else {
		// This is an exisiting object which has been reconciled before.
		result, err = reconciler.OnExistingObject(ctx, obj, sigNozClient, hash)
	}

	status.ReconciledAt = metav1.Now()

	if apiequality.Semantic.DeepEqual(beforeStatus, obj.GetCoreStatus()) {
		return ctrl.Result{}, nil
	}

	if err := reconciler.client.Status().Update(ctx, obj.K8sObject()); err != nil {
		return ctrl.Result{}, err
	}

	return result, err
}

func (reconciler *commonReconciler) OnNewObject(ctx context.Context, obj resources.Object, c clients.SigNoz, identity, hash string) (ctrl.Result, error) {
	logger := logf.FromContext(ctx).WithName("CommonReconciler.OnNewObject")

	status := obj.GetCoreStatus()
	generation := obj.K8sObject().GetGeneration()

	resourceMetadataCandidates, err := reconciler.adapter.Find(ctx, c, obj)
	if err != nil {
		logger.Error(err, "Failed to find object(s)")
		return reconciler.OnAdapterOperationErr(obj, err)
	}

	candidateIDs := resourcesv1alpha1.GetIDsFromSigNozResources(resourceMetadataCandidates)

	logger.Info("Found candidates for object", "candidates", candidateIDs)

	// If AnnotationSigNozResourceID is set, it takes precedence and we need to match the found candidate IDs with this annotation.
	if pinned := obj.K8sObject().GetAnnotations()[resourcesv1alpha1.AnnotationSigNozResourceID]; pinned != "" {
		for _, resourceMetadata := range resourceMetadataCandidates {
			id, err := resourcesv1alpha1.GetIDFromSigNozResource(resourceMetadata)
			if err == nil {
				if id == pinned {
					return reconciler.adopt(ctx, obj, c, resourceMetadata, hash)
				}
			}
		}

		resources.SetConditionsOnOutcome(
			status,
			generation,
			resources.ReconcilerOutcomeTerminal,
			resources.ReasonSigNozResourceIDMismatch,
			fmt.Sprintf("annotation %s: no object with id %q matches identity %q%s", resourcesv1alpha1.AnnotationSigNozResourceID, pinned, identity, strings.Join(candidateIDs, ", ")),
		)
		return ctrl.Result{}, nil
	}

	// Exactly 1 candidate is present, adopt it
	if len(resourceMetadataCandidates) == 1 {
		return reconciler.adopt(ctx, obj, c, resourceMetadataCandidates[0], hash)
	}

	if len(resourceMetadataCandidates) == 0 {
		// No candidate is present, create the resource
		return reconciler.create(ctx, obj, c, identity, hash)
	}

	// More than 1 candidate is present, the controller cannot do anything
	resources.SetConditionsOnOutcome(
		status,
		generation,
		resources.ReconcilerOutcomeTerminal,
		resources.ReasonAmbiguous,
		fmt.Sprintf("ambiguous: %d objects in SigNoz match identity %q%s; set annotation %s to the id of the one to adopt", len(resourceMetadataCandidates), identity, strings.Join(candidateIDs, ", "), resourcesv1alpha1.AnnotationSigNozResourceID),
	)

	return ctrl.Result{}, nil
}

func (reconciler *commonReconciler) OnExistingObject(ctx context.Context, obj resources.Object, c clients.SigNoz, hash string) (ctrl.Result, error) {
	logger := logf.FromContext(ctx).WithName("CommonReconciler.OnExistingObject")

	status := obj.GetCoreStatus()
	generation := obj.K8sObject().GetGeneration()
	resourceMetadata := status.SigNozResource

	remote, err := reconciler.adapter.Read(ctx, c, obj, resourceMetadata)
	if err != nil {
		logger.Error(err, "Failed to read object", "resourceMetadata", resourceMetadata)
		return reconciler.OnAdapterOperationErr(obj, err)
	}

	if errors.IsNotFound(err) {
		// The remote is gone. Drop the stale metadata and requeue.
		status.SigNozResource = nil
		status.ObservedHash = ""
		resources.SetConditionsOnOutcome(
			status,
			generation,
			resources.ReconcilerOutcomePending,
			resources.ReasonPending,
			"The SigNoz object was not found; it will be recreated",
		)

		return ctrl.Result{RequeueAfter: reconciler.requeueAfterOnNotFound}, nil
	}

	compareResult, err := obj.Compare(remote)
	if err != nil {
		// For some internal reason, compare has failed in the operator. This is likely a bug in the operator and needs to be fixed.
		resources.SetConditionsOnOutcome(
			status,
			generation,
			resources.ReconcilerOutcomeTerminal,
			resources.ReasonCompareFailed,
			"This is likely a bug in the operator: "+err.Error(),
		)

		return ctrl.Result{RequeueAfter: reconciler.retryInterval(obj)}, nil
	}

	if len(compareResult.ImmutableFields) > 0 {
		resources.SetConditionsOnOutcome(
			status,
			generation,
			resources.ReconcilerOutcomeTerminal,
			resources.ReasonImmutableFieldChanged,
			fmt.Sprintf("Immutable fields changed: %s. For this object, fields: %s are immutable. Revert the change, or delete and recreate the resource", strings.Join(compareResult.ImmutableFields, ", "), strings.Join(obj.ImmutableFields(), ",")),
		)

		return ctrl.Result{}, nil
	}

	// Has the live state changed at the server? This is computed by diffing the spec with the read response of the object
	isChangedByCompare := len(compareResult.UpdatableFields) != 0

	// Has the hash changed? This is computed by the previous spec hash and the current spec hash
	isChangedByHash := status.ObservedHash != "" && status.ObservedHash != hash

	if !isChangedByCompare && !isChangedByHash {
		// If neither the hash nor compare say anything is changed, we do nothing.
		status.ObservedHash = hash // for when current hash is empty
		resources.SetConditionsOnOutcome(
			status,
			generation,
			resources.ReconcilerOutcomeSynced,
			resources.ReasonSynced,
			"In sync with SigNoz",
		)

		return ctrl.Result{RequeueAfter: reconciler.interval(obj)}, nil
	}

	// Even if one disagrees, update:
	// - If compare disagrees and hash agrees, that means the fields have been edited on SigNoz.
	// - If compare agrees and hash disagrees, resource changed in a way that compare couldn't see - unobservable field (most likely new fields added in SigNoz but not yet in operator) or mapping gap. The hash's reason to exist.
	// - If compare disagress and hash disagrees, that means the fields have been edited on SigNoz and possibly by another operator.
	if err := reconciler.adapter.Update(ctx, c, obj, resourceMetadata); err != nil {
		logger.Error(err, "Failed to update object", "resourceMetadata", resourceMetadata)
		return reconciler.OnAdapterOperationErr(obj, err)
	}

	status.ObservedHash = hash
	resources.SetConditionsOnOutcome(
		status,
		generation,
		resources.ReconcilerOutcomeSynced,
		resources.ReasonUpdated,
		"Updated in SigNoz",
	)

	return ctrl.Result{RequeueAfter: reconciler.interval(obj)}, nil
}

func (reconciler *commonReconciler) OnConflict(ctx context.Context, obj resources.Object, c clients.SigNoz, identity, hash string) (ctrl.Result, error) {
	logger := logf.FromContext(ctx).WithName("CommonReconciler.OnConflict")
	status := obj.GetCoreStatus()
	generation := obj.K8sObject().GetGeneration()

	resourceMetadataCandidates, err := reconciler.adapter.Find(ctx, c, obj)
	if err != nil {
		logger.Error(err, "Failed to find object(s)")
		return reconciler.OnAdapterOperationErr(obj, err)
	}

	candidateIDs := resourcesv1alpha1.GetIDsFromSigNozResources(resourceMetadataCandidates)

	// Only 1 candidate was found, adopt it.
	if len(resourceMetadataCandidates) == 1 {
		return reconciler.adopt(ctx, obj, c, resourceMetadataCandidates[0], hash)
	}

	if len(resourceMetadataCandidates) == 0 {
		// Remove the create attempt first
		if err := reconciler.removeCreateAttemptAnnotation(ctx, obj); err != nil {
			return ctrl.Result{}, err
		}

		resources.SetConditionsOnOutcome(
			status,
			generation,
			resources.ReconcilerOutcomeTerminal,
			resources.ReasonRejected,
			fmt.Sprintf("SigNoz reported a conflict for identity %q but no matching object was found", identity),
		)
		return ctrl.Result{}, nil
	}

	resources.SetConditionsOnOutcome(
		status,
		generation,
		resources.ReconcilerOutcomeTerminal,
		resources.ReasonAmbiguous,
		fmt.Sprintf("ambiguous: %d objects in SigNoz match identity %q%s; set annotation %s to the id of the one to adopt", len(resourceMetadataCandidates), identity, strings.Join(candidateIDs, ", "), resourcesv1alpha1.AnnotationSigNozResourceID),
	)

	return ctrl.Result{}, nil
}

func (reconciler *commonReconciler) create(ctx context.Context, obj resources.Object, c clients.SigNoz, identity, hash string) (ctrl.Result, error) {
	logger := logf.FromContext(ctx, "CommonReconciler.create")
	status := obj.GetCoreStatus()
	generation := obj.K8sObject().GetGeneration()

	if err := reconciler.addCreateAttemptAnnotation(ctx, obj); err != nil {
		logger.Error(err, "Failed to add create attempt annotation")
		return ctrl.Result{}, err
	}

	resourceMetadata, err := reconciler.adapter.Create(ctx, c, obj)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			return reconciler.OnConflict(ctx, obj, c, identity, hash)
		}

		if !errors.IsRetryable(err) {
			// If the error is not retryable, remove the create attempt annotation.
			if rerr := reconciler.removeCreateAttemptAnnotation(ctx, obj); rerr != nil {
				return ctrl.Result{}, rerr
			}
		}

		return reconciler.OnAdapterOperationErr(obj, err)
	}

	if err := reconciler.removeCreateAttemptAnnotation(ctx, obj); err != nil {
		return ctrl.Result{}, err
	}

	status.SigNozResource = resourceMetadata
	status.ObservedHash = hash
	resources.SetConditionsOnOutcome(
		status,
		generation,
		resources.ReconcilerOutcomeSynced,
		resources.ReasonCreated,
		"Created in SigNoz",
	)

	return ctrl.Result{RequeueAfter: reconciler.interval(obj)}, nil
}

func (reconciler *commonReconciler) adopt(ctx context.Context, obj resources.Object, c clients.SigNoz, resourceMetadata *resourcesv1alpha1.SigNozResource, hash string) (ctrl.Result, error) {
	if err := reconciler.removeCreateAttemptAnnotation(ctx, obj); err != nil {
		return ctrl.Result{}, err
	}

	obj.GetCoreStatus().SigNozResource = resourceMetadata

	return reconciler.OnExistingObject(ctx, obj, c, hash)
}

func (reconciler *commonReconciler) finalize(ctx context.Context, obj resources.Object) (ctrl.Result, error) {
	logger := logf.FromContext(ctx).WithName("CommonReconciler.finalize")

	if !controllerutil.ContainsFinalizer(obj.K8sObject(), resourcesv1alpha1.ResourceFinalizer) {
		return ctrl.Result{}, nil
	}

	spec := obj.GetCoreSpec()

	if spec.ReclaimPolicy == resourcesv1alpha1.ReclaimOrphan {
		return reconciler.removeFinalizer(ctx, obj)
	}

	c, err := reconciler.resolveProviderConfig(ctx, obj, spec)
	if err != nil {
		requeueAfter := reconciler.retryInterval(obj)
		logger.Info("Waiting to reclaim the SigNoz object, its provider config is not resolvable", "requeueAfter", requeueAfter)

		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	}

	resourceMetadata := obj.GetCoreStatus().SigNozResource

	_, err = resourcesv1alpha1.GetIDFromSigNozResource(resourceMetadata)
	if err == nil {
		return reconciler.delete(ctx, c, obj, resourceMetadata)
	}

	// An unconfirmed create may have left an object behind. Resolve it by
	// identity, and only drop the finalizer when a lookup positively confirms
	// there is nothing to reclaim — never on a failed or ambiguous lookup,
	// which would orphan the remote object the reclaim policy meant to delete.
	identity, err := obj.Identity()
	if err != nil {
		logger.Error(err, "Cannot derive an identity to reclaim the SigNoz object by; removing finalizer")
		return reconciler.removeFinalizer(ctx, obj)
	}

	found, ferr := reconciler.adapter.Find(ctx, c, obj)
	if ferr != nil {
		logger.Error(ferr, "Could not look up the SigNoz object to reclaim; will retry")
		return ctrl.Result{RequeueAfter: reconciler.retryInterval(obj)}, nil
	}

	if len(found) == 0 {
		// The object does not exist, just remove the finalizer
		return reconciler.removeFinalizer(ctx, obj)
	}

	if len(found) == 1 {
		return reconciler.delete(ctx, c, obj, found[0])
	}

	logger.Info("Ambiguous SigNoz objects match this resource; will not guess which to reclaim", "count", len(found), "identity", identity)
	return ctrl.Result{RequeueAfter: reconciler.retryInterval(obj)}, nil
}

func (reconciler *commonReconciler) delete(ctx context.Context, c clients.SigNoz, obj resources.Object, resourceMetadata *resourcesv1alpha1.SigNozResource) (ctrl.Result, error) {
	logger := logf.FromContext(ctx).WithName("CommonReconciler.delete")

	if err := reconciler.adapter.Delete(ctx, c, obj, resourceMetadata); err != nil && !errors.IsNotFound(err) {
		logger.Error(err, "Could not reclaim the SigNoz object")
		return ctrl.Result{RequeueAfter: reconciler.retryInterval(obj)}, nil
	}

	logger.Info("Reclaimed the SigNoz object")
	return reconciler.removeFinalizer(ctx, obj)
}

func (reconciler *commonReconciler) addFinalizer(ctx context.Context, obj resources.Object) error {
	k8sObject := obj.K8sObject()

	ok := controllerutil.AddFinalizer(k8sObject, resourcesv1alpha1.ResourceFinalizer)
	if !ok {
		return nil
	}

	return reconciler.client.Update(ctx, k8sObject)
}

func (reconciler *commonReconciler) removeFinalizer(ctx context.Context, obj resources.Object) (ctrl.Result, error) {
	k8sObject := obj.K8sObject()

	ok := controllerutil.RemoveFinalizer(k8sObject, resourcesv1alpha1.ResourceFinalizer)
	if !ok {
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, reconciler.client.Update(ctx, k8sObject)
}

func (reconciler *commonReconciler) resolveProviderConfig(ctx context.Context, obj resources.Object, spec *resourcesv1alpha1.CoreSpec) (clients.SigNoz, error) {
	resolved, err := reconciler.resolver.ResolveRef(ctx, spec.ProviderConfigRef, obj.K8sObject().GetNamespace(), reconciler.operatorNamespace)
	if err != nil {
		return nil, err
	}

	return clients.New(resolved), nil
}

func (reconciler *commonReconciler) OnAdapterOperationErr(obj resources.Object, err error) (ctrl.Result, error) {
	outcome := resources.GetOutcomeAndSetConditionsOnErr(obj.GetCoreStatus(), obj.K8sObject().GetGeneration(), err)
	if outcome == resources.ReconcilerOutcomeRecoverable {
		return ctrl.Result{RequeueAfter: reconciler.retryInterval(obj)}, nil
	}

	return ctrl.Result{}, nil
}

func (reconciler *commonReconciler) addCreateAttemptAnnotation(ctx context.Context, obj resources.Object) error {
	k8sObject := obj.K8sObject()
	patch := client.MergeFrom(k8sObject.DeepCopyObject().(client.Object))

	annotations := k8sObject.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	annotations[resourcesv1alpha1.AnnotationCreateAttempt] = time.Now().UTC().Format(time.RFC3339)
	k8sObject.SetAnnotations(annotations)

	return reconciler.client.Patch(ctx, k8sObject, patch)
}

func (reconciler *commonReconciler) removeCreateAttemptAnnotation(ctx context.Context, obj resources.Object) error {
	k8sObject := obj.K8sObject()

	if _, ok := k8sObject.GetAnnotations()[resourcesv1alpha1.AnnotationCreateAttempt]; !ok {
		return nil
	}

	patch := client.MergeFrom(k8sObject.DeepCopyObject().(client.Object))

	annotations := k8sObject.GetAnnotations()
	delete(annotations, resourcesv1alpha1.AnnotationCreateAttempt)
	k8sObject.SetAnnotations(annotations)

	return reconciler.client.Patch(ctx, k8sObject, patch)
}

func (reconciler *commonReconciler) timeout(obj resources.Object) time.Duration {
	if d := obj.GetCoreSpec().Timeout; d != nil && d.Duration > 0 {
		return d.Duration
	}

	return reconciler.defaultTimeout
}

func (reconciler *commonReconciler) interval(obj resources.Object) time.Duration {
	if d := obj.GetCoreSpec().Interval; d != nil && d.Duration > 0 {
		return d.Duration
	}

	return reconciler.defaultInterval
}

func (reconciler *commonReconciler) retryInterval(obj resources.Object) time.Duration {
	spec := obj.GetCoreSpec()

	if d := spec.RetryInterval; d != nil && d.Duration > 0 {
		return d.Duration
	}

	if d := spec.Interval; d != nil && d.Duration > 0 {
		return d.Duration
	}

	if reconciler.defaultRetryInterval > 0 {
		return reconciler.defaultRetryInterval
	}

	return reconciler.defaultInterval
}
