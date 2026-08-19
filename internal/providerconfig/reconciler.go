package providerconfig

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	resourcesv1alpha1 "github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
)

// Reconciler resolves one provider config and reports the outcome on its Ready
// condition. It is the whole of both provider-config controllers' reconcile: the
// two kinds share one spec and one status, and differ only in refNamespace, the
// namespace their references resolve in.
type Reconciler interface {
	Reconcile(ctx context.Context, obj client.Object, spec *resourcesv1alpha1.ProviderConfigSpec, status *resourcesv1alpha1.ProviderConfigStatus, refNamespace string) (ctrl.Result, error)
}
