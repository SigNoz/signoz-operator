package resources

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	resourcesv1alpha1 "github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/providerconfig"
)

// ProviderConfigReconciler reconciles a ProviderConfig object.
type ProviderConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	CommonReconciler providerconfig.Reconciler
}

// +kubebuilder:rbac:groups=resources.signoz.io,namespace=signoz-operator-system,resources=providerconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=resources.signoz.io,namespace=signoz-operator-system,resources=providerconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=resources.signoz.io,namespace=signoz-operator-system,resources=providerconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups="",namespace=signoz-operator-system,resources=secrets;configmaps,verbs=get;list;watch

// Reconcile reports on the Ready condition whether the endpoint and credential this
// ProviderConfig names resolved. References resolve in the ProviderConfig's own
// namespace, so RBAC on the Secret there decides who writes through this backend.
func (r *ProviderConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	config := &resourcesv1alpha1.ProviderConfig{}
	if err := r.Get(ctx, req.NamespacedName, config); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// A provider config holds no remote state, so deletion needs no finalizer.
	if !config.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	return r.CommonReconciler.Reconcile(ctx, config, &config.Spec, &config.Status, config.Namespace)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ProviderConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := indexReferences(mgr, &resourcesv1alpha1.ProviderConfig{}, func(o client.Object) *resourcesv1alpha1.ProviderConfigSpec {
		return &o.(*resourcesv1alpha1.ProviderConfig).Spec
	}); err != nil {
		return fmt.Errorf("could not index ProviderConfig references: %w", err)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&resourcesv1alpha1.ProviderConfig{}).
		Watches(&corev1.Secret{}, watchProviderConfigReferences(r.Client, secretRefsIndex)).
		Watches(&corev1.ConfigMap{}, watchProviderConfigReferences(r.Client, configMapRefsIndex)).
		Named("resources-providerconfig").
		Complete(r)
}
