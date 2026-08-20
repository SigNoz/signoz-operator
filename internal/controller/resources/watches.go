package resources

import (
	"context"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// requestsMatching lists the objects newList holds under opts and maps each to
// a reconcile request. A failed list enqueues nothing.
func requestsMatching(ctx context.Context, c client.Client, newList func() client.ObjectList, opts ...client.ListOption) []reconcile.Request {
	list := newList()

	if err := c.List(ctx, list, opts...); err != nil {
		logf.FromContext(ctx).Error(err, "Could not list resources to requeue")

		return nil
	}

	items, err := apimeta.ExtractList(list)
	if err != nil {
		logf.FromContext(ctx).Error(err, "Could not extract items from resource list")

		return nil
	}

	requests := make([]reconcile.Request, 0, len(items))

	for _, item := range items {
		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(item.(client.Object)),
		})
	}

	return requests
}
