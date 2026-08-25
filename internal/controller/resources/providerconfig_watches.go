package resources

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	resourcesv1alpha1 "github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
)

// providerConfigRefIndex indexes a mirrored resource by the provider config it
// names, keyed "<kind>/<name>", so a config whose status moves requeues its
// referrers in one list. The index is registered per kind through
// indexProviderConfigRef; the watches below query it.
const providerConfigRefIndex = ".spec.providerConfigRef"

func providerConfigRefKey(kind, name string) string {
	if kind == "" {
		kind = "ProviderConfig"
	}

	return kind + "/" + name
}

func indexProviderConfigRef(mgr ctrl.Manager, obj client.Object, ref func(client.Object) resourcesv1alpha1.ProviderConfigRef) error {
	indexer := func(o client.Object) []string {
		r := ref(o)

		return []string{providerConfigRefKey(r.Kind, r.Name)}
	}

	return mgr.GetFieldIndexer().IndexField(context.Background(), obj, providerConfigRefIndex, indexer)
}

func watchProviderConfigs(c client.Client, newList func() client.ObjectList) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(enqueueReferencers(c, "ProviderConfig", newList, true))
}

func watchClusterProviderConfigs(c client.Client, newList func() client.ObjectList) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(enqueueReferencers(c, "ClusterProviderConfig", newList, false))
}

func enqueueReferencers(c client.Client, kind string, newList func() client.ObjectList, scoped bool) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		opts := []client.ListOption{
			client.MatchingFields{providerConfigRefIndex: providerConfigRefKey(kind, obj.GetName())},
		}

		if scoped {
			opts = append(opts, client.InNamespace(obj.GetNamespace()))
		}

		return requestsMatching(ctx, c, newList, opts...)
	}
}
