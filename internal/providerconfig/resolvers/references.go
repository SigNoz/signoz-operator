package resolvers

import (
	resourcesv1alpha1 "github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
)

// SecretNames lists the Secrets a provider config spec reads — endpoint,
// credential and CA bundle — de-duplicated and in reading order. The controllers
// index provider configs under these names, so a Secret event maps back to its
// readers in one list.
func SecretNames(spec *resourcesv1alpha1.ProviderConfigSpec) []string {
	names := make([]string, 0, 3)

	for _, src := range valueSources(spec) {
		if src != nil && src.SecretKeyRef != nil {
			names = append(names, src.SecretKeyRef.Name)
		}
	}

	if spec.TLS != nil && spec.TLS.CASecretRef != nil {
		names = append(names, spec.TLS.CASecretRef.Name)
	}

	return dedupe(names)
}

// ConfigMapNames is SecretNames for the ConfigMaps a spec reads.
func ConfigMapNames(spec *resourcesv1alpha1.ProviderConfigSpec) []string {
	names := make([]string, 0, 2)

	for _, src := range valueSources(spec) {
		if src != nil && src.ConfigMapKeyRef != nil {
			names = append(names, src.ConfigMapKeyRef.Name)
		}
	}

	return dedupe(names)
}

// valueSources lists the value-or-valueFrom fields of a spec, nil entries
// included.
func valueSources(spec *resourcesv1alpha1.ProviderConfigSpec) []*resourcesv1alpha1.ValueSource {
	sources := []*resourcesv1alpha1.ValueSource{spec.Endpoint.ValueFrom}

	if spec.Auth.Header != nil {
		sources = append(sources, spec.Auth.Header.ValueFrom)
	}

	return sources
}

// dedupe drops repeated and empty names, keeping first-seen order: one Secret
// referenced twice, as when a spec keeps the endpoint and the token in the same
// Secret, appears once.
func dedupe(names []string) []string {
	deduped := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))

	for _, name := range names {
		if name == "" {
			continue
		}

		if _, ok := seen[name]; ok {
			continue
		}

		seen[name] = struct{}{}
		deduped = append(deduped, name)
	}

	return deduped
}
