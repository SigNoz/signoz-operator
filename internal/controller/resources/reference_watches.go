package resources

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	resourcesv1alpha1 "github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/providerconfig"
)

// indexReferences registers the shared Secret/ConfigMap reference index for one
// provider-config kind; spec reads the shared spec off the kind's own type.
func indexReferences(mgr ctrl.Manager, obj client.Object, spec func(client.Object) *resourcesv1alpha1.ProviderConfigSpec) error {
	indexer := func(o client.Object) []string {
		return providerconfig.ReferenceKeys(spec(o))
	}

	return mgr.GetFieldIndexer().IndexField(context.Background(), obj, providerconfig.IndexField, indexer)
}

// watchProviderConfigReferences requeues, on a Secret or ConfigMap event, the
// ProviderConfigs in that object's namespace which read it, so a rotated
// credential is re-resolved and the resources naming the config see its status
// move.
func watchProviderConfigReferences(c client.Client, kind providerconfig.ReferenceKind) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		return requestsMatching(ctx, c,
			func() client.ObjectList { return &resourcesv1alpha1.ProviderConfigList{} },
			client.InNamespace(obj.GetNamespace()),
			client.MatchingFields{providerconfig.IndexField: providerconfig.Key(kind, obj.GetName())})
	})
}

// watchClusterProviderConfigReferences requeues, on a Secret or ConfigMap event,
// the ClusterProviderConfigs that read it. Only the operator's own namespace
// matters: that is the one place a cluster-scoped config's references resolve.
func watchClusterProviderConfigReferences(c client.Client, kind providerconfig.ReferenceKind, operatorNamespace string) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		if obj.GetNamespace() != operatorNamespace {
			return nil
		}

		return requestsMatching(ctx, c,
			func() client.ObjectList { return &resourcesv1alpha1.ClusterProviderConfigList{} },
			client.MatchingFields{providerconfig.IndexField: providerconfig.Key(kind, obj.GetName())})
	})
}
