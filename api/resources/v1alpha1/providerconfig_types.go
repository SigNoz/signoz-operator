package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// ProviderConfigRef names the backend a resource writes through. Kind
// ProviderConfig resolves in the resource's own namespace; ClusterProviderConfig
// resolves cluster-wide. There is no namespace field.
type ProviderConfigRef struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// +optional
	// +kubebuilder:validation:Enum=ProviderConfig;ClusterProviderConfig
	// +kubebuilder:default=ProviderConfig
	Kind string `json:"kind,omitempty"`
}

// ValueSource is the analog of corev1.EnvVarSource without the pod-only
// fieldRef/resourceFieldRef. References are name-only, so they resolve in the
// object's own namespace.
// +kubebuilder:validation:MinProperties=1
// +kubebuilder:validation:MaxProperties=1
type ValueSource struct {
	// +optional
	SecretKeyRef *corev1.SecretKeySelector `json:"secretKeyRef,omitempty"`

	// +optional
	ConfigMapKeyRef *corev1.ConfigMapKeySelector `json:"configMapKeyRef,omitempty"`
}

// Endpoint is an inline value, or valueFrom a Secret/ConfigMap.
// +kubebuilder:validation:XValidation:rule="has(self.value) != has(self.valueFrom)",message="set exactly one of value or valueFrom"
type Endpoint struct {
	// +kubebuilder:validation:Pattern=`^https?://.+$`
	// +optional
	Value string `json:"value,omitempty"`

	// +optional
	ValueFrom *ValueSource `json:"valueFrom,omitempty"`
}

// HeaderAuth sends one HTTP header carrying the credential, inline or valueFrom
// a Secret/ConfigMap.
// +kubebuilder:validation:XValidation:rule="has(self.value) != has(self.valueFrom)",message="set exactly one of value or valueFrom"
type HeaderAuth struct {
	// Defaults to SIGNOZ-API-KEY.
	// +kubebuilder:default=SIGNOZ-API-KEY
	// +optional
	Name string `json:"name,omitempty"`

	// Scheme, if set, is prepended with a single space, producing "<scheme> <value>" —
	// e.g. Scheme "Bearer" with Name "Authorization" sends "Authorization: Bearer <value>".
	// +optional
	Scheme string `json:"scheme,omitempty"`

	// +optional
	Value string `json:"value,omitempty"`

	// +optional
	ValueFrom *ValueSource `json:"valueFrom,omitempty"`
}

// Authentication is a closed union of methods; exactly one must be set.
// +kubebuilder:validation:MinProperties=1
// +kubebuilder:validation:MaxProperties=1
type Authentication struct {
	// +optional
	Header *HeaderAuth `json:"header,omitempty"`
}

// TLSConfig configures trust for a self-hosted endpoint behind a private CA.
type TLSConfig struct {
	// +optional
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`

	// CASecretRef names a Secret key holding a CA bundle to trust.
	// +optional
	CASecretRef *corev1.SecretKeySelector `json:"caSecretRef,omitempty"`
}

// ProviderConfigSpec is the shared spec of ProviderConfig and ClusterProviderConfig.
type ProviderConfigSpec struct {
	// Endpoint is the SigNoz base URL.
	// +required
	Endpoint Endpoint `json:"endpoint"`

	// +required
	Auth Authentication `json:"auth"`

	// +optional
	TLS *TLSConfig `json:"tls,omitempty"`
}

// ProviderConfigStatus is the shared observed state of ProviderConfig and ClusterProviderConfig.
type ProviderConfigStatus struct {
	// A Ready condition reports that the endpoint and credential resolved. It does not report that SigNoz answered.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the metadata.generation the conditions were set from.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ObservedRefVersions records the resourceVersion observed for each Secret
	// and ConfigMap this object reads, keyed by kind, then by
	// "<namespace>/<name>". It holds no credential material.
	// +optional
	ObservedRefVersions map[string]map[string]string `json:"observedRefVersions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ProviderConfig names one SigNoz backend and how to authenticate to it,
// resolved in the referencing resource's own namespace.
type ProviderConfig struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ProviderConfig
	// +required
	Spec ProviderConfigSpec `json:"spec"`

	// status defines the observed state of ProviderConfig
	// +optional
	Status ProviderConfigStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ProviderConfigList contains a list of ProviderConfig
type ProviderConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ProviderConfig `json:"items"`
}

func (s *ProviderConfigSpec) SecretNames() map[string]struct{} {
	names := make(map[string]struct{}, 3)

	for _, src := range s.ValueSources() {
		if src != nil && src.SecretKeyRef != nil && src.SecretKeyRef.Name != "" {
			names[src.SecretKeyRef.Name] = struct{}{}
		}
	}

	return names
}

func (s *ProviderConfigSpec) ConfigMapNames() map[string]struct{} {
	names := make(map[string]struct{}, 2)

	for _, src := range s.ValueSources() {
		if src != nil && src.ConfigMapKeyRef != nil && src.ConfigMapKeyRef.Name != "" {
			names[src.ConfigMapKeyRef.Name] = struct{}{}
		}
	}

	return names
}

func (s *ProviderConfigSpec) ValueSources() []*ValueSource {
	sources := []*ValueSource{s.Endpoint.ValueFrom}

	if s.Auth.Header != nil {
		sources = append(sources, s.Auth.Header.ValueFrom)
	}

	if s.TLS != nil && s.TLS.CASecretRef != nil {
		sources = append(sources, &ValueSource{SecretKeyRef: s.TLS.CASecretRef})
	}

	return sources
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ProviderConfig{}, &ProviderConfigList{})
		return nil
	})
}
