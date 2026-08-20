package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ReclaimPolicy controls what happens to the SigNoz object when the custom
// resource that mirrors it is deleted.
// +kubebuilder:validation:Enum=Delete;Orphan
type ReclaimPolicy string

const (
	// ReclaimDelete removes the SigNoz object when the custom resource is deleted.
	ReclaimDelete ReclaimPolicy = "Delete"

	// ReclaimOrphan leaves the SigNoz object in place.
	ReclaimOrphan ReclaimPolicy = "Orphan"
)

// CoreSpec holds the controls every mirrored resource shares, independent of
// what the resource is. Each kind embeds it inline, so these fields sit at the
// same path on every kind. See docs/core-spec.md.
type CoreSpec struct {
	// ProviderConfigRef names the SigNoz backend to write through, resolved in
	// the resource's own namespace unless its kind is ClusterProviderConfig.
	// +kubebuilder:validation:Required
	ProviderConfigRef ProviderConfigRef `json:"providerConfigRef"`

	// Interval is the steady-state cadence at which the resource is re-checked
	// against SigNoz. Omitted, it falls back to the operator-wide default.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern=`^([0-9]+(\.[0-9]+)?(ms|s|m|h))+$`
	// +optional
	Interval *metav1.Duration `json:"interval,omitempty"`

	// RetryInterval is the cadence at which a Recoverable failure is retried.
	// Omitted, it falls back to Interval, then to the operator-wide default.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern=`^([0-9]+(\.[0-9]+)?(ms|s|m|h))+$`
	// +optional
	RetryInterval *metav1.Duration `json:"retryInterval,omitempty"`

	// Timeout bounds a single reconciliation attempt, including the HTTP calls
	// to SigNoz. Omitted, it falls back to the operator-wide default.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern=`^([0-9]+(\.[0-9]+)?(ms|s|m|h))+$`
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`

	// Suspend stops reconciling this resource without changing or deleting
	// anything in SigNoz.
	// +optional
	Suspend bool `json:"suspend,omitempty"`

	// ReclaimPolicy controls what happens in SigNoz when this custom resource is
	// deleted. Defaults to Delete.
	// +kubebuilder:default=Delete
	// +optional
	ReclaimPolicy ReclaimPolicy `json:"reclaimPolicy,omitempty"`
}

// ResourceMetadata records the identity of the object this resource maps to in
// SigNoz. It is a nested struct rather than a bare field, so a further
// identifier can be added without a schema break. See docs/core-status.md.
type ResourceMetadata struct {
	// ID is the identifier SigNoz assigned, set once the operator has created or
	// adopted the remote object.
	// +optional
	ID *string `json:"id,omitempty"`
}

// CoreStatus is the observed-state counterpart of CoreSpec, embedded inline by
// every mirrored kind. See docs/core-status.md.
type CoreStatus struct {
	// Conditions report the reconcile outcome. The type vocabulary is fixed and
	// shared across kinds; per-kind detail lives in reason and message.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// SigNozResourceMetadata records the object's identity in SigNoz. Its ID is
	// empty until a create or lookup confirms it. It is a cache, not the source
	// of truth for identity: if lost, the next reconcile re-discovers it.
	// +optional
	SigNozResourceMetadata *ResourceMetadata `json:"signozResourceMetadata,omitempty"`

	// ObservedGeneration is the metadata.generation the operator last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ObservedHash is a hash of the last body the operator sent to SigNoz. It is
	// an optimisation that lets a reconcile skip the remote fetch when desired
	// state is unchanged, never the drift mechanism.
	// +optional
	ObservedHash string `json:"observedHash,omitempty"`

	// ReconciledAt is when the operator last reconciled the resource against
	// SigNoz.
	// +optional
	ReconciledAt metav1.Time `json:"reconciledAt,omitzero"`
}
