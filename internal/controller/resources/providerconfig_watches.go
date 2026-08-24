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

// providerConfigRefKey builds the index key for a reference, defaulting a blank
// kind to ProviderConfig exactly as the schema does.
func providerConfigRefKey(kind, name string) string {
	if kind == "" {
		kind = "ProviderConfig"
	}

	return kind + "/" + name
}

// indexProviderConfigRef registers the shared provider-config index for one
// kind; ref reads the reference off the kind's own type.
func indexProviderConfigRef(mgr ctrl.Manager, obj client.Object, ref func(client.Object) resourcesv1alpha1.ProviderConfigRef) error {
	indexer := func(o client.Object) []string {
		r := ref(o)

		return []string{providerConfigRefKey(r.Kind, r.Name)}
	}

	return mgr.GetFieldIndexer().IndexField(context.Background(), obj, providerConfigRefIndex, indexer)
}

// watchProviderConfigs requeues, on a namespaced ProviderConfig event, the
// resources of one kind in the config's own namespace that name it, so a
// resolved or rotated credential reaches them. newList builds the kind's empty
// list.
func watchProviderConfigs(c client.Client, newList func() client.ObjectList) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(enqueueReferencers(c, "ProviderConfig", newList, true))
}

// watchClusterProviderConfigs requeues, on a ClusterProviderConfig event, the
// resources of one kind in any namespace that name it — a cluster-scoped
// config has no namespace of its own.
func watchClusterProviderConfigs(c client.Client, newList func() client.ObjectList) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(enqueueReferencers(c, "ClusterProviderConfig", newList, false))
}

// enqueueReferencers is the map function behind both watches: list, via the
// shared index, the resources of one kind naming the provider config that
// fired the event, and enqueue them. scoped limits the lookup to the config's
// own namespace.
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
