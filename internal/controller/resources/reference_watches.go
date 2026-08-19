package resources

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	resourcesv1alpha1 "github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/providerconfig/resolvers"
)

// The field indexes under which both provider-config kinds list the objects they
// read, keyed by plain name, one index per referenced kind.
const (
	secretRefsIndex    = ".spec.secretRefs"
	configMapRefsIndex = ".spec.configMapRefs"
)

// indexReferences registers the Secret and ConfigMap reference indexes for one
// provider-config kind; spec reads the shared spec off the kind's own type.
func indexReferences(mgr ctrl.Manager, obj client.Object, spec func(client.Object) *resourcesv1alpha1.ProviderConfigSpec) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), obj, secretRefsIndex, func(o client.Object) []string {
		return resolvers.SecretNames(spec(o))
	}); err != nil {
		return err
	}

	return mgr.GetFieldIndexer().IndexField(context.Background(), obj, configMapRefsIndex, func(o client.Object) []string {
		return resolvers.ConfigMapNames(spec(o))
	})
}

// watchProviderConfigReferences requeues, on a Secret or ConfigMap event, the
// ProviderConfigs in that object's namespace which read it, so a rotated
// credential is re-resolved and the resources naming the config see its status
// move. indexField is the reference index matching the watched kind.
func watchProviderConfigReferences(c client.Client, indexField string) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		return requestsMatching(ctx, c,
			func() client.ObjectList { return &resourcesv1alpha1.ProviderConfigList{} },
			client.InNamespace(obj.GetNamespace()),
			client.MatchingFields{indexField: obj.GetName()})
	})
}

// watchClusterProviderConfigReferences requeues, on a Secret or ConfigMap event,
// the ClusterProviderConfigs that read it. Only the operator's own namespace
// matters: that is the one place a cluster-scoped config's references resolve.
func watchClusterProviderConfigReferences(c client.Client, indexField string, operatorNamespace string) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		if obj.GetNamespace() != operatorNamespace {
			return nil
		}

		return requestsMatching(ctx, c,
			func() client.ObjectList { return &resourcesv1alpha1.ClusterProviderConfigList{} },
			client.MatchingFields{indexField: obj.GetName()})
	})
}
