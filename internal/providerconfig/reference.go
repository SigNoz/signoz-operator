package providerconfig

import (
	resourcesv1alpha1 "github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
)

// IndexField is the field-index name under which both provider-config kinds index
// the objects they read, so a credential change maps back to its readers in one list.
const IndexField = ".spec.references"

// The kinds of object a provider config reads a value from.
var (
	ReferenceKindSecret    = ReferenceKind{s: "Secret"}
	ReferenceKindConfigMap = ReferenceKind{s: "ConfigMap"}
)

// ReferenceKind is one of the kinds above; the unexported field keeps the
// vocabulary closed.
type ReferenceKind struct{ s string }

func (k ReferenceKind) String() string {
	return k.s
}

// Reference is one Secret or ConfigMap a provider config reads.
type Reference struct {
	Kind ReferenceKind
	Name string
}

// Key is the "<kind>/<name>" form that keys both the field index and
// status.observedRefVersions.
func (r Reference) Key() string {
	return r.Kind.String() + "/" + r.Name
}

// Key builds a reference key from a kind and a name.
func Key(kind ReferenceKind, name string) string {
	return Reference{Kind: kind, Name: name}.Key()
}

// References lists every Secret and ConfigMap the spec reads — endpoint, credential
// and CA bundle — de-duplicated and in a stable order. One object referenced twice,
// as when a spec keeps the endpoint and the token in the same Secret, appears once.
func References(spec *resourcesv1alpha1.ProviderConfigSpec) []Reference {
	refs := make([]Reference, 0, 3)
	seen := make(map[string]struct{}, 3)

	add := func(ref Reference) {
		if ref.Name == "" {
			return
		}

		if _, ok := seen[ref.Key()]; ok {
			return
		}

		seen[ref.Key()] = struct{}{}
		refs = append(refs, ref)
	}

	addSource := func(src *resourcesv1alpha1.ValueSource) {
		if src == nil {
			return
		}

		if src.SecretKeyRef != nil {
			add(Reference{Kind: ReferenceKindSecret, Name: src.SecretKeyRef.Name})
		}

		if src.ConfigMapKeyRef != nil {
			add(Reference{Kind: ReferenceKindConfigMap, Name: src.ConfigMapKeyRef.Name})
		}
	}

	addSource(spec.Endpoint.ValueFrom)

	if spec.Auth.Header != nil {
		addSource(spec.Auth.Header.ValueFrom)
	}

	if spec.TLS != nil && spec.TLS.CASecretRef != nil {
		add(Reference{Kind: ReferenceKindSecret, Name: spec.TLS.CASecretRef.Name})
	}

	return refs
}

// ReferenceKeys is References as index keys.
func ReferenceKeys(spec *resourcesv1alpha1.ProviderConfigSpec) []string {
	refs := References(spec)

	keys := make([]string, 0, len(refs))
	for _, ref := range refs {
		keys = append(keys, ref.Key())
	}

	return keys
}
