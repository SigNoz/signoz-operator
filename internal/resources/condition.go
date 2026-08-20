package resources

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
)

// Condition types are fixed and shared across every mirrored kind, so
// `kubectl wait --for=condition=Ready` and generic tooling work uniformly.
// Per-kind and per-case detail lives in the reason and message, never the type.
// The vocabulary is private: callers hand SetConditions a ReconcilerOutcome and
// this package owns what appears on status. See docs/core-status.md.
var (
	// conditionReady is the single condition a user or tool should wait on. It is
	// derived, rolling up the others by precedence.
	conditionReady = condition{s: "Ready", isAlwaysPresent: true}

	// conditionSynced is three-valued: True when the remote matches desired,
	// False when a Terminal cause means it never will, Unknown otherwise.
	conditionSynced = condition{s: "Synced", isAlwaysPresent: true}

	// conditionTerminal marks desired state a retry will not fix. A stable state:
	// the reconciler settles here and does not requeue.
	conditionTerminal = condition{s: "Terminal", isAlwaysPresent: false}

	// conditionRecoverable marks a transient failure, retried at retryInterval.
	conditionRecoverable = condition{s: "Recoverable", isAlwaysPresent: false}

	// conditionSuspended marks reconciliation paused by spec.suspend.
	conditionSuspended = condition{s: "Suspended", isAlwaysPresent: false}
)

// condition is one of the fixed condition types above. isAlwaysPresent
// distinguishes the two rendering kinds: an always-present condition is
// three-valued and never removed, while a marker is present (True) only while
// it applies and removed otherwise. Markers are mutually exclusive by
// construction — they render a single-valued ReconcilerOutcome, so two can
// never hold at once.
type condition struct {
	s               string
	isAlwaysPresent bool
}

// SetConditions renders one reconciler outcome onto status: Synced is always
// present and three-valued; exactly the marker matching the outcome — among
// Suspended, Terminal and Recoverable — is present (True) and the others are
// removed; Ready is derived so a reader waits on one condition. See
// docs/core-status.md.
func SetConditions(status *v1alpha1.CoreStatus, generation int64, outcome ReconcilerOutcome, reason Reason, message string) {
	// Stamp here rather than earlier in the reconcile: a metadata patch re-reads
	// the object and drops status set before it, so observedGeneration must be
	// written alongside the conditions, which are always the last status mutation.
	status.ObservedGeneration = generation

	synced := metav1.ConditionUnknown
	switch outcome {
	case ReconcilerOutcomeSynced:
		synced = metav1.ConditionTrue
	case ReconcilerOutcomeTerminal:
		synced = metav1.ConditionFalse
	}

	apply(status, generation, conditionSynced, synced, reason, message)
	apply(status, generation, conditionSuspended, presence(outcome == ReconcilerOutcomeSuspended), reason, message)
	apply(status, generation, conditionTerminal, presence(outcome == ReconcilerOutcomeTerminal), reason, message)
	apply(status, generation, conditionRecoverable, presence(outcome == ReconcilerOutcomeRecoverable), reason, message)

	ready := metav1.ConditionUnknown
	switch outcome {
	case ReconcilerOutcomeSynced:
		ready = metav1.ConditionTrue
	case ReconcilerOutcomeTerminal, ReconcilerOutcomeSuspended:
		ready = metav1.ConditionFalse
	}

	apply(status, generation, conditionReady, ready, reason, message)
}

// apply writes one condition according to its rendering kind: an always-present
// condition is written whatever its status, while a marker that does not hold
// is removed rather than written False — presence is its signal.
func apply(status *v1alpha1.CoreStatus, generation int64, c condition, s metav1.ConditionStatus, reason Reason, message string) {
	if !c.isAlwaysPresent && s != metav1.ConditionTrue {
		meta.RemoveStatusCondition(&status.Conditions, c.s)

		return
	}

	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               c.s,
		Status:             s,
		Reason:             reason.String(),
		Message:            message,
		ObservedGeneration: generation,
	})
}

func presence(on bool) metav1.ConditionStatus {
	if on {
		return metav1.ConditionTrue
	}

	return metav1.ConditionFalse
}
