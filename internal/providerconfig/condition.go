package providerconfig

import (
	"errors"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	resourcesv1alpha1 "github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
)

// conditionReady means the operator read the object, resolved the endpoint and
// the credential, and could assemble an authenticated request from them. It does
// not mean SigNoz answered. The vocabulary is private: callers hand SetConditions
// a resolution outcome and this package owns what appears on status.
const conditionReady = "Ready"

// SetConditions renders one resolution outcome onto status: Ready True when the
// spec resolved, False with the failure's reason and message otherwise. The
// observed generation and reference versions are stamped alongside, so status
// always describes one coherent observation.
func SetConditions(status *resourcesv1alpha1.ProviderConfigStatus, generation int64, versions map[string]string, resolveErr error) {
	ready := metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             ReasonResolved.String(),
		Message:            "Endpoint and credential resolved",
		ObservedGeneration: generation,
	}

	var failure *Error

	switch {
	case resolveErr == nil:
	case errors.As(resolveErr, &failure):
		ready.Status = metav1.ConditionFalse
		ready.Reason = failure.Reason.String()
		ready.Message = failure.Message
	default:
		ready.Status = metav1.ConditionFalse
		ready.Reason = ReasonReferenceReadFailed.String()
		ready.Message = resolveErr.Error()
	}

	meta.SetStatusCondition(&status.Conditions, ready)
	status.ObservedGeneration = generation

	status.ObservedRefVersions = nil
	if len(versions) > 0 {
		status.ObservedRefVersions = versions
	}
}
