package providerconfig

import (
	"errors"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	resourcesv1alpha1 "github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
)

// ConditionReady means the endpoint and credential resolved and an authenticated
// request could be assembled — not that SigNoz answered.
const ConditionReady = "Ready"

// SetConditions renders one resolution outcome onto status: Ready True when the
// spec resolved, False with the failure's reason and message otherwise.
func SetConditions(status *resourcesv1alpha1.ProviderConfigStatus, generation int64, versions map[resourcesv1alpha1.ProviderConfigObservedRefKind]map[client.ObjectKey]string, resolveErr error) {
	ready := metav1.Condition{
		Type:               ConditionReady,
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

	for kind, byKey := range versions {
		if len(byKey) == 0 {
			continue
		}

		if status.ObservedRefVersions == nil {
			status.ObservedRefVersions = make(map[resourcesv1alpha1.ProviderConfigObservedRefKind]map[string]string, len(versions))
		}

		observed := make(map[string]string, len(byKey))
		for key, version := range byKey {
			observed[key.String()] = version
		}

		status.ObservedRefVersions[kind] = observed
	}
}
