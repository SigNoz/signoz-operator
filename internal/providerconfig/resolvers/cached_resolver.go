package resolvers

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	resourcesv1alpha1 "github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/providerconfig"
)

// CachedResolver implements providerconfig.Resolver over a client.Reader. Reads
// cache within one Resolve call, never across calls: a later reconcile must
// re-read a rotated credential.
type CachedResolver struct {
	reader client.Reader
}

func NewCachedResolver(reader client.Reader) *CachedResolver {
	return &CachedResolver{reader: reader}
}

func (r *CachedResolver) ResolveRef(
	ctx context.Context,
	ref resourcesv1alpha1.ProviderConfigRef,
	resourceNamespace, operatorNamespace string,
) (*providerconfig.ResolvedProviderConfig, error) {
	var spec *resourcesv1alpha1.ProviderConfigSpec

	var refNamespace string

	switch ref.Kind {
	case "", "ProviderConfig":
		config := &resourcesv1alpha1.ProviderConfig{}

		if err := r.reader.Get(ctx, client.ObjectKey{Namespace: resourceNamespace, Name: ref.Name}, config); err != nil {
			return nil, fmt.Errorf("ProviderConfig %q in namespace %q: %w", ref.Name, resourceNamespace, err)
		}

		spec = &config.Spec
		refNamespace = resourceNamespace

	case "ClusterProviderConfig":
		config := &resourcesv1alpha1.ClusterProviderConfig{}

		if err := r.reader.Get(ctx, client.ObjectKey{Name: ref.Name}, config); err != nil {
			return nil, fmt.Errorf("ClusterProviderConfig %q: %w", ref.Name, err)
		}

		spec = &config.Spec
		refNamespace = operatorNamespace

	default:
		// Unreachable: the schema's enum on providerConfigRef.kind allows only the above.
		return nil, fmt.Errorf("providerConfigRef.kind %q is not a known provider config kind", ref.Kind)
	}

	resolved, _, err := r.Resolve(ctx, refNamespace, spec)
	if err != nil {
		return nil, fmt.Errorf("provider config %q could not be resolved: %w", ref.Name, err)
	}

	return resolved, nil
}

func (r *CachedResolver) Resolve(
	ctx context.Context,
	namespace string,
	spec *resourcesv1alpha1.ProviderConfigSpec,
) (*providerconfig.ResolvedProviderConfig, map[string]map[client.ObjectKey]string, error) {
	c := &cache{
		reader:     r.reader,
		namespace:  namespace,
		secrets:    map[client.ObjectKey]*corev1.Secret{},
		configMaps: map[client.ObjectKey]*corev1.ConfigMap{},
		errs:       map[string]map[client.ObjectKey]error{},
		versions:   map[string]map[client.ObjectKey]string{},
	}

	// Read every reference up front, so the observed versions are complete
	// whichever value later fails to validate.
	for name := range spec.SecretNames() {
		_, _ = c.secret(ctx, client.ObjectKey{Namespace: c.namespace, Name: name})
	}

	for name := range spec.ConfigMapNames() {
		_, _ = c.configMap(ctx, client.ObjectKey{Namespace: c.namespace, Name: name})
	}

	endpoint, err := c.endpoint(ctx, spec)
	if err != nil {
		return nil, c.versions, err
	}

	headerName, headerValue, err := c.credential(ctx, &spec.Auth)
	if err != nil {
		return nil, c.versions, err
	}

	insecure, caPool, err := c.trust(ctx, spec.TLS)
	if err != nil {
		return nil, c.versions, err
	}

	return &providerconfig.ResolvedProviderConfig{
		Endpoint:           endpoint,
		HeaderName:         headerName,
		HeaderValue:        headerValue,
		InsecureSkipVerify: insecure,
		CAPool:             caPool,
	}, c.versions, nil
}
