package providerconfig

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	resourcesv1alpha1 "github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
)

// DefaultHeaderName is the schema default for spec.auth.header.name.
const DefaultHeaderName = "SIGNOZ-API-KEY"

// Resolver reads the endpoint, credential and CA bundle a provider config names.
type Resolver interface {
	// Resolve resolves spec's references in namespace. The returned map records
	// the resourceVersion observed per reference, keyed by kind then object key,
	// for status.observedRefVersions; it is returned on failure too.
	Resolve(ctx context.Context, namespace string, spec *resourcesv1alpha1.ProviderConfigSpec) (*ResolvedProviderConfig, map[string]map[client.ObjectKey]string, error)

	// ResolveRef resolves the provider config ref names: kind ProviderConfig
	// reads its references in resourceNamespace, ClusterProviderConfig in
	// operatorNamespace.
	ResolveRef(ctx context.Context, ref resourcesv1alpha1.ProviderConfigRef, resourceNamespace, operatorNamespace string) (*ResolvedProviderConfig, error)
}
