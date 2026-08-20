package resources

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
)

type Reconciler interface {
	// Reconcile an obj against the SigNoz backend.
	Reconcile(ctx context.Context, obj Object) (ctrl.Result, error)
}
