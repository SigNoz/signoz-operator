package resources

import (
	"context"
	"errors"
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

// ClusterProviderConfigReconciler reconciles a ClusterProviderConfig object.
type ClusterProviderConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	CommonReconciler providerconfig.Reconciler

	// Namespace is the operator's own namespace. A ClusterProviderConfig has no
	// namespace of its own, so this is where its name-only Secret and ConfigMap
	// references resolve: the shared credential is one a cluster administrator
	// places beside the operator.
	Namespace string
}

// +kubebuilder:rbac:groups=resources.signoz.io,resources=clusterproviderconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=resources.signoz.io,resources=clusterproviderconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=resources.signoz.io,resources=clusterproviderconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups="",namespace=signoz-operator-system,resources=secrets;configmaps,verbs=get;list;watch

// Reconcile reports on the Ready condition whether the endpoint and credential this
// ClusterProviderConfig names resolved. References resolve in the operator's
// namespace, so the credential a cluster-wide backend writes with is one only a
// cluster administrator can place.
func (r *ClusterProviderConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	config := &resourcesv1alpha1.ClusterProviderConfig{}
	if err := r.Get(ctx, req.NamespacedName, config); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// A provider config holds no remote state, so deletion needs no finalizer.
	if !config.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	return r.CommonReconciler.Reconcile(ctx, config, &config.Spec, &config.Status, r.Namespace)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterProviderConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Namespace == "" {
		return errors.New("operator namespace is required to resolve ClusterProviderConfig references")
	}

	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &resourcesv1alpha1.ClusterProviderConfig{}, secretRefsIndex, func(o client.Object) []string {
		return slices.Collect(maps.Keys(o.(*resourcesv1alpha1.ClusterProviderConfig).Spec.SecretNames()))
	}); err != nil {
		return fmt.Errorf("could not index ClusterProviderConfig Secret references: %w", err)
	}

	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &resourcesv1alpha1.ClusterProviderConfig{}, configMapRefsIndex, func(o client.Object) []string {
		return slices.Collect(maps.Keys(o.(*resourcesv1alpha1.ClusterProviderConfig).Spec.ConfigMapNames()))
	}); err != nil {
		return fmt.Errorf("could not index ClusterProviderConfig ConfigMap references: %w", err)
	}

	// A Secret or ConfigMap event requeues the ClusterProviderConfigs that read
	// it. Only the operator's own namespace matters: that is the one place a
	// cluster-scoped config's references resolve.
	watchReferences := func(indexField string) handler.EventHandler {
		return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			if obj.GetNamespace() != r.Namespace {
				return nil
			}

			return requestsMatching(ctx, r.Client,
				func() client.ObjectList { return &resourcesv1alpha1.ClusterProviderConfigList{} },
				client.MatchingFields{indexField: obj.GetName()})
		})
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&resourcesv1alpha1.ClusterProviderConfig{}).
		Watches(&corev1.Secret{}, watchReferences(secretRefsIndex)).
		Watches(&corev1.ConfigMap{}, watchReferences(configMapRefsIndex)).
		Named("resources-clusterproviderconfig").
		Complete(r)
}
