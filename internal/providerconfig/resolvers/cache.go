package resolvers

import (
	"context"
	"crypto/x509"
	"fmt"
	"net/url"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	resourcesv1alpha1 "github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/errors"
	"github.com/SigNoz/signoz-operator/internal/providerconfig"
)

// cache backs one Resolve call: each referenced object is read once, its
// resourceVersion recorded, and a failed read replayed against every field that
// needs it. errs and versions key by kind first, disambiguating a Secret and
// ConfigMap sharing a name.
type cache struct {
	reader     client.Reader
	namespace  string
	secrets    map[client.ObjectKey]*corev1.Secret
	configMaps map[client.ObjectKey]*corev1.ConfigMap
	errs       map[resourcesv1alpha1.ProviderConfigObservedRefKind]map[client.ObjectKey]error
	versions   map[resourcesv1alpha1.ProviderConfigObservedRefKind]map[client.ObjectKey]string
}

func (c *cache) fail(kind resourcesv1alpha1.ProviderConfigObservedRefKind, objectKey client.ObjectKey, err error) error {
	if c.errs[kind] == nil {
		c.errs[kind] = map[client.ObjectKey]error{}
	}

	c.errs[kind][objectKey] = err

	return err
}

func (c *cache) observe(kind resourcesv1alpha1.ProviderConfigObservedRefKind, objectKey client.ObjectKey, version string) {
	if c.versions[kind] == nil {
		c.versions[kind] = map[client.ObjectKey]string{}
	}

	c.versions[kind][objectKey] = version
}

//nolint:dupl // deliberately parallel to configMap
func (c *cache) secret(ctx context.Context, objectKey client.ObjectKey) (*corev1.Secret, error) {
	if err, ok := c.errs[resourcesv1alpha1.ProviderConfigObservedRefKindSecret][objectKey]; ok {
		return nil, err
	}

	if secret, ok := c.secrets[objectKey]; ok {
		return secret, nil
	}

	secret := &corev1.Secret{}
	if err := c.reader.Get(ctx, objectKey, secret); err != nil {
		resolveErr := errors.Newf(errors.ReasonInternal, "could not read Secret %q in namespace %q: %s", objectKey.Name, objectKey.Namespace, err).WithCode(providerconfig.CodeReferenceReadFailed)
		if apierrors.IsNotFound(err) {
			resolveErr = errors.Newf(errors.ReasonNotFound, "Secret %q not found in namespace %q", objectKey.Name, objectKey.Namespace).WithCode(providerconfig.CodeSecretNotFound)
		}

		return nil, c.fail(resourcesv1alpha1.ProviderConfigObservedRefKindSecret, objectKey, resolveErr)
	}

	c.secrets[objectKey] = secret
	c.observe(resourcesv1alpha1.ProviderConfigObservedRefKindSecret, objectKey, secret.ResourceVersion)

	return secret, nil
}

//nolint:dupl // deliberately parallel to secret
func (c *cache) configMap(ctx context.Context, objectKey client.ObjectKey) (*corev1.ConfigMap, error) {
	if err, ok := c.errs[resourcesv1alpha1.ProviderConfigObservedRefKindConfigMap][objectKey]; ok {
		return nil, err
	}

	if configMap, ok := c.configMaps[objectKey]; ok {
		return configMap, nil
	}

	configMap := &corev1.ConfigMap{}
	if err := c.reader.Get(ctx, objectKey, configMap); err != nil {
		resolveErr := errors.Newf(errors.ReasonInternal, "could not read ConfigMap %q in namespace %q: %s", objectKey.Name, objectKey.Namespace, err).WithCode(providerconfig.CodeReferenceReadFailed)
		if apierrors.IsNotFound(err) {
			resolveErr = errors.Newf(errors.ReasonNotFound, "ConfigMap %q not found in namespace %q", objectKey.Name, objectKey.Namespace).WithCode(providerconfig.CodeConfigMapNotFound)
		}

		return nil, c.fail(resourcesv1alpha1.ProviderConfigObservedRefKindConfigMap, objectKey, resolveErr)
	}

	c.configMaps[objectKey] = configMap
	c.observe(resourcesv1alpha1.ProviderConfigObservedRefKindConfigMap, objectKey, configMap.ResourceVersion)

	return configMap, nil
}

func (c *cache) endpoint(ctx context.Context, spec *resourcesv1alpha1.ProviderConfigSpec) (*url.URL, error) {
	const path = "spec.endpoint"

	raw, err := c.value(ctx, path, spec.Endpoint.Value, spec.Endpoint.ValueFrom)
	if err != nil {
		return nil, err
	}

	// No message carries any part of the value, which url.Parse would quote back:
	// an endpoint may be sourced from the same Secret as the credential.
	endpoint, parseErr := url.Parse(raw)
	switch {
	case parseErr != nil:
		return nil, errors.New(errors.ReasonInvalidInput, path+": value is not a valid URL").WithCode(providerconfig.CodeEndpointInvalid)
	case endpoint.Scheme != "http" && endpoint.Scheme != "https":
		return nil, errors.New(errors.ReasonInvalidInput, path+": URL scheme must be http or https").WithCode(providerconfig.CodeEndpointInvalid)
	case endpoint.Host == "":
		return nil, errors.New(errors.ReasonInvalidInput, path+": URL has no host").WithCode(providerconfig.CodeEndpointInvalid)
	}

	return endpoint, nil
}

// authentication resolves the one method set into the header it sends.
func (c *cache) authentication(ctx context.Context, auth *resourcesv1alpha1.Authentication) (providerconfig.ResolvedAuthentication, error) {
	if auth.Header == nil {
		return providerconfig.ResolvedAuthentication{}, errors.New(errors.ReasonInvalidInput, "spec.auth: no authentication method set").WithCode(providerconfig.CodeSpecInvalid)
	}

	const path = "spec.auth.header"

	header := auth.Header

	value, err := c.value(ctx, path, header.Value, header.ValueFrom)
	if err != nil {
		return providerconfig.ResolvedAuthentication{}, err
	}

	if value == "" {
		return providerconfig.ResolvedAuthentication{}, errors.New(errors.ReasonInvalidInput, path+": credential resolved to an empty value").WithCode(providerconfig.CodeValueEmpty)
	}

	name := header.Name
	if name == "" {
		name = providerconfig.DefaultHeaderName
	}

	return providerconfig.ResolvedAuthentication{
		Header: &providerconfig.ResolvedHeaderAuthentication{Name: name, Scheme: header.Scheme, Value: value},
	}, nil
}

func (c *cache) trust(ctx context.Context, config *resourcesv1alpha1.TLSConfig) (*providerconfig.ResolvedTLSConfig, error) {
	if config == nil {
		return nil, nil
	}

	resolved := &providerconfig.ResolvedTLSConfig{InsecureSkipVerify: config.InsecureSkipVerify}

	if config.CASecretRef == nil {
		return resolved, nil
	}

	const path = "spec.tls.caSecretRef"

	ref := config.CASecretRef

	secret, err := c.secret(ctx, client.ObjectKey{Namespace: c.namespace, Name: ref.Name})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	bundle, ok := secret.Data[ref.Key]
	if !ok {
		return nil, errors.Newf(errors.ReasonNotFound, "%s: Secret %q has no key %q", path, ref.Name, ref.Key).WithCode(providerconfig.CodeKeyNotFound)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(bundle) {
		return nil, errors.Newf(errors.ReasonInvalidInput, "%s: Secret %q key %q holds no PEM certificate", path, ref.Name, ref.Key).WithCode(providerconfig.CodeCABundleInvalid)
	}

	resolved.CAPool = pool

	return resolved, nil
}

// value returns a field's inline or sourced value, with path naming the field on
// failure. Whitespace is trimmed: a file-sourced Secret key ends in a newline
// that would make every request unsendable.
func (c *cache) value(
	ctx context.Context,
	path string,
	inline string,
	from *resourcesv1alpha1.ValueSource,
) (string, error) {
	if from == nil {
		if inline == "" {
			return "", errors.New(errors.ReasonInvalidInput, path+": set exactly one of value or valueFrom").WithCode(providerconfig.CodeSpecInvalid)
		}

		return strings.TrimSpace(inline), nil
	}

	switch {
	case from.SecretKeyRef != nil:
		ref := from.SecretKeyRef
		fieldPath := path + ".valueFrom.secretKeyRef"

		secret, err := c.secret(ctx, client.ObjectKey{Namespace: c.namespace, Name: ref.Name})
		if err != nil {
			return "", fmt.Errorf("%s: %w", fieldPath, err)
		}

		value, ok := secret.Data[ref.Key]
		if !ok {
			return "", errors.Newf(errors.ReasonNotFound, "%s: Secret %q has no key %q", fieldPath, ref.Name, ref.Key).WithCode(providerconfig.CodeKeyNotFound)
		}

		return strings.TrimSpace(string(value)), nil

	case from.ConfigMapKeyRef != nil:
		ref := from.ConfigMapKeyRef
		fieldPath := path + ".valueFrom.configMapKeyRef"

		configMap, err := c.configMap(ctx, client.ObjectKey{Namespace: c.namespace, Name: ref.Name})
		if err != nil {
			return "", fmt.Errorf("%s: %w", fieldPath, err)
		}

		value, ok := configMap.Data[ref.Key]
		if !ok {
			return "", errors.Newf(errors.ReasonNotFound, "%s: ConfigMap %q has no key %q", fieldPath, ref.Name, ref.Key).WithCode(providerconfig.CodeKeyNotFound)
		}

		return strings.TrimSpace(value), nil

	default:
		return "", errors.New(errors.ReasonInvalidInput, path+".valueFrom: set exactly one of secretKeyRef or configMapKeyRef").WithCode(providerconfig.CodeSpecInvalid)
	}
}
