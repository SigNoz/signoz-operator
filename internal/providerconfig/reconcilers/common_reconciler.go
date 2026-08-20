package reconcilers

import (
	"context"
	"fmt"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	resourcesv1alpha1 "github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/providerconfig"
)

type CommonReconciler struct {
	client   client.Client
	resolver providerconfig.Resolver
}

func NewCommonReconciler(c client.Client, resolver providerconfig.Resolver) *CommonReconciler {
	return &CommonReconciler{client: c, resolver: resolver}
}

func (reconciler *CommonReconciler) Reconcile(ctx context.Context, obj client.Object, spec *resourcesv1alpha1.ProviderConfigSpec, status *resourcesv1alpha1.ProviderConfigStatus, refNamespace string) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	_, versions, err := reconciler.resolver.Resolve(ctx, refNamespace, spec)

	observed := status.DeepCopy()

	providerconfig.SetConditions(status, obj.GetGeneration(), versions, err)

	if !apiequality.Semantic.DeepEqual(observed, status) {
		if err := reconciler.client.Status().Update(ctx, obj); err != nil {
			return ctrl.Result{}, fmt.Errorf("could not update status: %w", err)
		}

		if ready := meta.FindStatusCondition(status.Conditions, providerconfig.ConditionReady); ready != nil {
			log.Info("Reconciled provider config", "ready", ready.Status, "reason", ready.Reason)
		}
	}

	// Every failure retries: backoff bounds a permanent one, while waiting on a
	// watch could strand a transient one.
	return ctrl.Result{}, err
}
