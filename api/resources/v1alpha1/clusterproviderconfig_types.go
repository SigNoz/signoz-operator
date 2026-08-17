package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// ClusterProviderConfig is the cluster-scoped twin of ProviderConfig over the
// same spec. Its Secret and ConfigMap references resolve in the operator's own
// namespace rather than a referencing resource's.
type ClusterProviderConfig struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ClusterProviderConfig
	// +required
	Spec ProviderConfigSpec `json:"spec"`

	// status defines the observed state of ClusterProviderConfig
	// +optional
	Status ProviderConfigStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ClusterProviderConfigList contains a list of ClusterProviderConfig
type ClusterProviderConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ClusterProviderConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ClusterProviderConfig{}, &ClusterProviderConfigList{})
		return nil
	})
}
