package resolvers

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	resourcesv1alpha1 "github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/providerconfig"
)

// The kinds keying the versions and errs maps, as they appear in
// status.observedRefVersions.
const (
	kindSecret    = "Secret"
	kindConfigMap = "ConfigMap"
)

// CachedResolver resolves provider config specs by reading their Secret and
// ConfigMap references through a client.Reader. Reads are cached per Resolve call,
// never across calls: the cache exists so one resolution observes each reference
// exactly once, not to save reads, and a later reconcile must re-read a rotated
// credential.
type CachedResolver struct {
	reader client.Reader
}

func NewCachedResolver(reader client.Reader) *CachedResolver {
	return &CachedResolver{reader: reader}
}

// ResolveRef resolves the provider config a resource names. Kind ProviderConfig
// resolves its references in the resource's own namespace; ClusterProviderConfig
// resolves them in the operator's. This is the single home of that rule.
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
		// The schema's enum on providerConfigRef.kind makes this unreachable;
		// report it rather than guess.
		return nil, fmt.Errorf("providerConfigRef.kind %q is not a known provider config kind", ref.Kind)
	}

	resolved, _, err := r.Resolve(ctx, refNamespace, spec)
	if err != nil {
		return nil, fmt.Errorf("provider config %q could not be resolved: %w", ref.Name, err)
	}

	return resolved, nil
}

// Resolve implements providerconfig.Resolver over one fresh per-call cache.
//
// The returned map records the resourceVersion observed for every reference that
// was read, keyed by "<kind>/<name>", for status.observedRefVersions. It is
// returned on failure too, so status reports what was read before the failure.
func (r *CachedResolver) Resolve(
	ctx context.Context,
	namespace string,
	spec *resourcesv1alpha1.ProviderConfigSpec,
) (*providerconfig.ResolvedProviderConfig, map[string]map[client.ObjectKey]string, error) {
	load := &loading{
		reader:     r.reader,
		namespace:  namespace,
		secrets:    map[client.ObjectKey]*corev1.Secret{},
		configMaps: map[client.ObjectKey]*corev1.ConfigMap{},
		errs:       map[string]map[client.ObjectKey]error{},
		versions:   map[string]map[client.ObjectKey]string{},
	}

	// Read every reference up front, so the observed versions are complete whichever
	// value later fails to validate. Failures are cached and returned from the field
	// that needs them below.
	for _, name := range SecretNames(spec) {
		_, _ = load.secret(ctx, load.objectKey(name))
	}

	for _, name := range ConfigMapNames(spec) {
		_, _ = load.configMap(ctx, load.objectKey(name))
	}

	endpoint, err := load.endpoint(ctx, spec)
	if err != nil {
		return nil, load.versions, err
	}

	headerName, headerValue, err := load.credential(ctx, &spec.Auth)
	if err != nil {
		return nil, load.versions, err
	}

	insecure, caPool, err := load.trust(ctx, spec.TLS)
	if err != nil {
		return nil, load.versions, err
	}

	return &providerconfig.ResolvedProviderConfig{
		Endpoint:           endpoint,
		HeaderName:         headerName,
		HeaderValue:        headerValue,
		InsecureSkipVerify: insecure,
		CAPool:             caPool,
	}, load.versions, nil
}

// loading is the cache behind one Resolve call. It reads the objects a provider
// config references, once each, recording the resourceVersion it saw and caching
// a failure so that it is reported against every field that needed it. Objects
// cache under their client.ObjectKey; errs and versions key by kind first, which
// disambiguates a Secret and ConfigMap sharing a name.
type loading struct {
	reader     client.Reader
	namespace  string
	secrets    map[client.ObjectKey]*corev1.Secret
	configMaps map[client.ObjectKey]*corev1.ConfigMap
	errs       map[string]map[client.ObjectKey]error
	versions   map[string]map[client.ObjectKey]string
}

// objectKey locates one name-only reference: every reference resolves in the
// loading's single namespace.
func (l *loading) objectKey(name string) client.ObjectKey {
	return client.ObjectKey{Namespace: l.namespace, Name: name}
}

// fail caches a failed read under its kind and key, and returns it.
func (l *loading) fail(kind string, objectKey client.ObjectKey, err error) error {
	if l.errs[kind] == nil {
		l.errs[kind] = map[client.ObjectKey]error{}
	}

	l.errs[kind][objectKey] = err

	return err
}

// observe records the resourceVersion one read saw under its kind and key.
func (l *loading) observe(kind string, objectKey client.ObjectKey, version string) {
	if l.versions[kind] == nil {
		l.versions[kind] = map[client.ObjectKey]string{}
	}

	l.versions[kind][objectKey] = version
}

func (l *loading) secret(ctx context.Context, objectKey client.ObjectKey) (*corev1.Secret, error) {
	if err, ok := l.errs[kindSecret][objectKey]; ok {
		return nil, err
	}

	if secret, ok := l.secrets[objectKey]; ok {
		return secret, nil
	}

	secret := &corev1.Secret{}
	if err := l.reader.Get(ctx, objectKey, secret); err != nil {
		return nil, l.fail(kindSecret, objectKey, fmt.Errorf("could not read Secret %q in namespace %q: %w", objectKey.Name, objectKey.Namespace, err))
	}

	l.secrets[objectKey] = secret
	l.observe(kindSecret, objectKey, secret.ResourceVersion)

	return secret, nil
}

func (l *loading) configMap(ctx context.Context, objectKey client.ObjectKey) (*corev1.ConfigMap, error) {
	if err, ok := l.errs[kindConfigMap][objectKey]; ok {
		return nil, err
	}

	if configMap, ok := l.configMaps[objectKey]; ok {
		return configMap, nil
	}

	configMap := &corev1.ConfigMap{}
	if err := l.reader.Get(ctx, objectKey, configMap); err != nil {
		return nil, l.fail(kindConfigMap, objectKey, fmt.Errorf("could not read ConfigMap %q in namespace %q: %w", objectKey.Name, objectKey.Namespace, err))
	}

	l.configMaps[objectKey] = configMap
	l.observe(kindConfigMap, objectKey, configMap.ResourceVersion)

	return configMap, nil
}

func (l *loading) endpoint(ctx context.Context, spec *resourcesv1alpha1.ProviderConfigSpec) (*url.URL, error) {
	const path = "spec.endpoint"

	raw, err := l.value(ctx, path, spec.Endpoint.Value, spec.Endpoint.ValueFrom)
	if err != nil {
		return nil, err
	}

	// The messages below carry no part of the value, which url.Parse would quote back:
	// an endpoint may be sourced from the same Secret as the credential, and a sourced
	// endpoint is one the user chose to keep out of the spec.
	endpoint, parseErr := url.Parse(raw)
	switch {
	case parseErr != nil:
		return nil, errors.New(path + ": value is not a valid URL")
	case endpoint.Scheme != "http" && endpoint.Scheme != "https":
		return nil, errors.New(path + ": URL scheme must be http or https")
	case endpoint.Host == "":
		return nil, errors.New(path + ": URL has no host")
	}

	return endpoint, nil
}

// credential resolves the one authentication method that is set into the header name
// and value to send.
func (l *loading) credential(ctx context.Context, auth *resourcesv1alpha1.Authentication) (string, string, error) {
	if auth.Header == nil {
		return "", "", errors.New("spec.auth: no authentication method set")
	}

	const path = "spec.auth.header"

	header := auth.Header

	value, err := l.value(ctx, path, header.Value, header.ValueFrom)
	if err != nil {
		return "", "", err
	}

	if value == "" {
		return "", "", errors.New(path + ": credential resolved to an empty value")
	}

	name := header.Name
	if name == "" {
		name = providerconfig.DefaultHeaderName
	}

	if header.Scheme != "" {
		value = header.Scheme + " " + value
	}

	return name, value, nil
}

func (l *loading) trust(ctx context.Context, config *resourcesv1alpha1.TLSConfig) (bool, *x509.CertPool, error) {
	if config == nil {
		return false, nil, nil
	}

	if config.CASecretRef == nil {
		return config.InsecureSkipVerify, nil, nil
	}

	const path = "spec.tls.caSecretRef"

	ref := config.CASecretRef

	secret, err := l.secret(ctx, l.objectKey(ref.Name))
	if err != nil {
		return config.InsecureSkipVerify, nil, fmt.Errorf("%s: %w", path, err)
	}

	bundle, ok := secret.Data[ref.Key]
	if !ok {
		return config.InsecureSkipVerify, nil, fmt.Errorf("%s: Secret %q has no key %q", path, ref.Name, ref.Key)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(bundle) {
		return config.InsecureSkipVerify, nil, fmt.Errorf("%s: Secret %q key %q holds no PEM certificate", path, ref.Name, ref.Key)
	}

	return config.InsecureSkipVerify, pool, nil
}

// value returns a value given inline or sourced from a Secret or ConfigMap key, with
// path naming the field for the message on failure.
//
// Surrounding whitespace is trimmed: neither a credential nor a URL can carry any,
// and the trailing newline a file-sourced Secret key ends up with would otherwise
// make every request unsendable.
func (l *loading) value(
	ctx context.Context,
	path string,
	inline string,
	from *resourcesv1alpha1.ValueSource,
) (string, error) {
	if from == nil {
		if inline == "" {
			return "", errors.New(path + ": set exactly one of value or valueFrom")
		}

		return strings.TrimSpace(inline), nil
	}

	switch {
	case from.SecretKeyRef != nil:
		ref := from.SecretKeyRef
		fieldPath := path + ".valueFrom.secretKeyRef"

		secret, err := l.secret(ctx, l.objectKey(ref.Name))
		if err != nil {
			return "", fmt.Errorf("%s: %w", fieldPath, err)
		}

		value, ok := secret.Data[ref.Key]
		if !ok {
			return "", fmt.Errorf("%s: Secret %q has no key %q", fieldPath, ref.Name, ref.Key)
		}

		return strings.TrimSpace(string(value)), nil

	case from.ConfigMapKeyRef != nil:
		ref := from.ConfigMapKeyRef
		fieldPath := path + ".valueFrom.configMapKeyRef"

		configMap, err := l.configMap(ctx, l.objectKey(ref.Name))
		if err != nil {
			return "", fmt.Errorf("%s: %w", fieldPath, err)
		}

		value, ok := configMap.Data[ref.Key]
		if !ok {
			return "", fmt.Errorf("%s: ConfigMap %q has no key %q", fieldPath, ref.Name, ref.Key)
		}

		return strings.TrimSpace(value), nil

	default:
		return "", errors.New(path + ".valueFrom: set exactly one of secretKeyRef or configMapKeyRef")
	}
}
