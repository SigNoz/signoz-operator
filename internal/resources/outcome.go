package resources

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

var (
	AdapterOutcomeTerminal    = AdapterOutcome{s: "terminal"}    // a retry will not fix it
	AdapterOutcomeRecoverable = AdapterOutcome{s: "recoverable"} // a retry may fix it
)

// AdapterOutcome is how an adapter classifies a failed HTTP outcome.
type AdapterOutcome struct{ s string }

func (o AdapterOutcome) String() string {
	return o.s
}
