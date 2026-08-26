package resources

import (
	"context"
	"fmt"
	"maps"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	resourcesv1alpha1 "github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/providerconfig"
)

// Field indexes both provider-config kinds register, keyed by referenced object name.
const (
	secretRefsIndex    = ".spec.secretRefs"
	configMapRefsIndex = ".spec.configMapRefs"
)

type ProviderConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	CommonReconciler providerconfig.Reconciler
}

// +kubebuilder:rbac:groups=resources.signoz.io,resources=providerconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=resources.signoz.io,resources=providerconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=resources.signoz.io,resources=providerconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets;configmaps,verbs=get;list;watch

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

func (r *ProviderConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &resourcesv1alpha1.ProviderConfig{}, secretRefsIndex, func(o client.Object) []string {
		return slices.Collect(maps.Keys(o.(*resourcesv1alpha1.ProviderConfig).Spec.SecretNames()))
	}); err != nil {
		return fmt.Errorf("could not index ProviderConfig Secret references: %w", err)
	}

	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &resourcesv1alpha1.ProviderConfig{}, configMapRefsIndex, func(o client.Object) []string {
		return slices.Collect(maps.Keys(o.(*resourcesv1alpha1.ProviderConfig).Spec.ConfigMapNames()))
	}); err != nil {
		return fmt.Errorf("could not index ProviderConfig ConfigMap references: %w", err)
	}

	// A Secret or ConfigMap event requeues the ProviderConfigs in its namespace
	// that read it, so a rotated credential is re-resolved.
	watchReferences := func(indexField string) handler.EventHandler {
		return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			return requestsMatching(ctx, r.Client,
				func() client.ObjectList { return &resourcesv1alpha1.ProviderConfigList{} },
				client.InNamespace(obj.GetNamespace()),
				client.MatchingFields{indexField: obj.GetName()})
		})
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&resourcesv1alpha1.ProviderConfig{}).
		Watches(&corev1.Secret{}, watchReferences(secretRefsIndex)).
		Watches(&corev1.ConfigMap{}, watchReferences(configMapRefsIndex)).
		Named("resources-providerconfig").
		Complete(r)
}
