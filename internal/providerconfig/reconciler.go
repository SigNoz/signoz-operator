package providerconfig

import (
	"context"
	"errors"
	"fmt"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	resourcesv1alpha1 "github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
)

// Reconcile resolves one provider config and reports the outcome on its Ready
// condition. It is the whole of both provider-config controllers' reconcile:
// the two kinds share one spec and one status, and differ only in the namespace
// their references resolve in.
//
// The resolved connection is discarded. This reconcile reports that one could be
// assembled; the credential in it must not reach a status, an event or a log line.
func Reconcile(
	ctx context.Context,
	c client.Client,
	obj client.Object,
	spec *resourcesv1alpha1.ProviderConfigSpec,
	status *resourcesv1alpha1.ProviderConfigStatus,
	refNamespace string,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	_, versions, resolveErr := Resolve(ctx, c, refNamespace, spec)

	observed := status.DeepCopy()

	SetConditions(status, obj.GetGeneration(), versions, resolveErr)

	if !apiequality.Semantic.DeepEqual(observed, status) {
		if err := c.Status().Update(ctx, obj); err != nil {
			return ctrl.Result{}, fmt.Errorf("could not update status: %w", err)
		}

		if ready := meta.FindStatusCondition(status.Conditions, conditionReady); ready != nil {
			log.Info("Reconciled provider config", "ready", ready.Status, "reason", ready.Reason)
		}
	}

	// A resolution failure is not requeued: the watches on the object and on the
	// Secrets and ConfigMaps it reads bring the reconcile back when the cause is
	// fixed. Only a failure not explicitly marked wait-for-watch is the
	// operator's to retry.
	var failure *Error
	if errors.As(resolveErr, &failure) && failure.Outcome != ResolveOutcomeWaitForWatch {
		return ctrl.Result{}, resolveErr
	}

	return ctrl.Result{}, nil
}
