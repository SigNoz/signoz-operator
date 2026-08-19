package providerconfig

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	resourcesv1alpha1 "github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
)

// DefaultHeaderName is the schema default for spec.auth.header.name.
const DefaultHeaderName = "SIGNOZ-API-KEY"

// Resolver reads the endpoint, credential and CA bundle a provider config spec
// names, resolving references in namespace, and returns the resolved config they
// describe.
//
// The returned map records the resourceVersion observed for every reference that
// was read, keyed by kind ("Secret", "ConfigMap"), then by object key, for
// status.observedRefVersions. It is returned on failure too, so status reports
// what was read before the failure.
type Resolver interface {
	Resolve(ctx context.Context, namespace string, spec *resourcesv1alpha1.ProviderConfigSpec) (*ResolvedProviderConfig, map[string]map[client.ObjectKey]string, error)
}
