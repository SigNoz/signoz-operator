package resources

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/errors"
)

var (
	// ConditionReady is the single condition a user or tool should wait on. It is derived, rolling up the others by precedence.
	ConditionReady = condition{s: "Ready", isAlwaysPresent: true}

	// ConditionSynced is three-valued: True when the remote matches desired, False when a Terminal cause means it never will, Unknown otherwise.
	ConditionSynced = condition{s: "Synced", isAlwaysPresent: true}

	// ConditionTerminal marks desired state a retry will not fix. A stable state: the reconciler settles here and does not requeue.
	ConditionTerminal = condition{s: "Terminal", isAlwaysPresent: false}

	// ConditionRecoverable marks a transient failure, retried at retryInterval.
	ConditionRecoverable = condition{s: "Recoverable", isAlwaysPresent: false}

	// ConditionSuspended marks reconciliation paused by spec.suspend.
	ConditionSuspended = condition{s: "Suspended", isAlwaysPresent: false}
)

type condition struct {
	s               string
	isAlwaysPresent bool
}

func (c condition) String() string {
	return c.s
}

func SetConditionsOnOutcome(status *v1alpha1.CoreStatus, generation int64, outcome ReconcilerOutcome, reason Reason, message string) {
	status.ObservedGeneration = generation

	synced := metav1.ConditionUnknown
	switch outcome {
	case ReconcilerOutcomeSynced:
		synced = metav1.ConditionTrue
	case ReconcilerOutcomeTerminal:
		synced = metav1.ConditionFalse
	}

	apply(status, generation, ConditionSynced, synced, reason, message)
	apply(status, generation, ConditionSuspended, presence(outcome == ReconcilerOutcomeSuspended), reason, message)
	apply(status, generation, ConditionTerminal, presence(outcome == ReconcilerOutcomeTerminal), reason, message)
	apply(status, generation, ConditionRecoverable, presence(outcome == ReconcilerOutcomeRecoverable), reason, message)

	ready := metav1.ConditionUnknown
	switch outcome {
	case ReconcilerOutcomeSynced:
		ready = metav1.ConditionTrue
	case ReconcilerOutcomeTerminal, ReconcilerOutcomeSuspended:
		ready = metav1.ConditionFalse
	}

	apply(status, generation, ConditionReady, ready, reason, message)
}

func GetOutcomeAndSetConditionsOnErr(status *v1alpha1.CoreStatus, generation int64, err error) ReconcilerOutcome {
	var base *errors.Base
	if !errors.As(err, &base) {
		SetConditionsOnOutcome(status, generation, ReconcilerOutcomeRecoverable, ReasonBackendUnreachable, err.Error())
		return ReconcilerOutcomeRecoverable
	}

	if errors.IsUnreachable(err) {
		SetConditionsOnOutcome(status, generation, ReconcilerOutcomeRecoverable, ReasonBackendUnreachable, err.Error())
		return ReconcilerOutcomeRecoverable
	}

	if errors.IsRetryable(err) {
		SetConditionsOnOutcome(status, generation, ReconcilerOutcomeRecoverable, ReasonBackendError, err.Error())
		return ReconcilerOutcomeRecoverable
	}

	if errors.IsUnauthorized(err) || errors.IsForbidden(err) {
		SetConditionsOnOutcome(status, generation, ReconcilerOutcomeTerminal, ReasonUnauthorized, err.Error())
		return ReconcilerOutcomeTerminal
	}

	SetConditionsOnOutcome(status, generation, ReconcilerOutcomeTerminal, ReasonRejected, err.Error())
	return ReconcilerOutcomeTerminal
}

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
