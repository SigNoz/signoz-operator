package resources

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	ReconcilerOutcomeSynced      = ReconcilerOutcome{s: "synced"}      // the remote matches desired
	ReconcilerOutcomePending     = ReconcilerOutcome{s: "pending"}     // the outcome is not yet known
	ReconcilerOutcomeRecoverable = ReconcilerOutcome{s: "recoverable"} // a transient failure
	ReconcilerOutcomeTerminal    = ReconcilerOutcome{s: "terminal"}    // a permanent failure
	ReconcilerOutcomeSuspended   = ReconcilerOutcome{s: "suspended"}   // paused by spec.suspend
)

// ReconcilerOutcome is the reconciler's verdict for one reconcile pass.
type ReconcilerOutcome struct{ s string }

func (o ReconcilerOutcome) String() string {
	return o.s
}

type Reconciler interface {
	// Reconcile an obj against the SigNoz backend.
	Reconcile(ctx context.Context, obj Object) (ctrl.Result, error)
}
