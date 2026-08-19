package providerconfig

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	resourcesv1alpha1 "github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
)

// DefaultHeaderName is the schema default for spec.auth.header.name.
const DefaultHeaderName = "SIGNOZ-API-KEY"

// ResolveRef resolves the provider config a resource names into a connection.
// Kind ProviderConfig resolves its references in the resource's own namespace;
// ClusterProviderConfig resolves them in the operator's. This is the single
// home of that rule.
func ResolveRef(
	ctx context.Context,
	reader client.Reader,
	ref resourcesv1alpha1.ProviderConfigReference,
	resourceNamespace, operatorNamespace string,
) (*Connection, error) {
	var spec *resourcesv1alpha1.ProviderConfigSpec

	var refNamespace string

	switch ref.Kind {
	case "", "ProviderConfig":
		config := &resourcesv1alpha1.ProviderConfig{}

		if err := reader.Get(ctx, types.NamespacedName{Namespace: resourceNamespace, Name: ref.Name}, config); err != nil {
			return nil, fmt.Errorf("ProviderConfig %q in namespace %q: %s", ref.Name, resourceNamespace, readMessage(err))
		}

		spec = &config.Spec
		refNamespace = resourceNamespace

	case "ClusterProviderConfig":
		config := &resourcesv1alpha1.ClusterProviderConfig{}

		if err := reader.Get(ctx, types.NamespacedName{Name: ref.Name}, config); err != nil {
			return nil, fmt.Errorf("ClusterProviderConfig %q: %s", ref.Name, readMessage(err))
		}

		spec = &config.Spec
		refNamespace = operatorNamespace

	default:
		// The schema's enum on providerConfigRef.kind makes this unreachable;
		// report it rather than guess.
		return nil, fmt.Errorf("providerConfigRef.kind %q is not a known provider config kind", ref.Kind)
	}

	conn, _, err := Resolve(ctx, reader, refNamespace, spec)
	if err != nil {
		return nil, fmt.Errorf("provider config %q could not be resolved: %w", ref.Name, err)
	}

	return conn, nil
}

// Resolve reads the endpoint, credential and CA bundle a provider config names, and
// returns the connection they describe. References resolve in namespace: a
// ProviderConfig's own namespace, the operator's for a ClusterProviderConfig.
//
// The returned map records the resourceVersion observed for every reference that was
// read, keyed by "<kind>/<name>", for status.observedRefVersions. It is returned on
// failure too, so status reports what was read before the failure. Every error is an
// *Error carrying the reason to report.
func Resolve(
	ctx context.Context,
	reader client.Reader,
	namespace string,
	spec *resourcesv1alpha1.ProviderConfigSpec,
) (*Connection, map[string]string, error) {
	load := &loader{
		reader:     reader,
		namespace:  namespace,
		secrets:    map[string]*corev1.Secret{},
		configMaps: map[string]*corev1.ConfigMap{},
		errs:       map[string]error{},
		versions:   map[string]string{},
	}

	// Read every reference up front, so the observed versions are complete whichever
	// value later fails to validate. Failures are cached and returned from the field
	// that needs them below.
	for _, ref := range References(spec) {
		switch ref.Kind {
		case ReferenceKindSecret:
			_, _ = load.secret(ctx, ref.Name)
		case ReferenceKindConfigMap:
			_, _ = load.configMap(ctx, ref.Name)
		}
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

	return &Connection{
		Endpoint:           endpoint,
		HeaderName:         headerName,
		HeaderValue:        headerValue,
		InsecureSkipVerify: insecure,
		CAPool:             caPool,
	}, load.versions, nil
}

// loader reads the objects a provider config references, once each, recording the
// resourceVersion it saw and caching a failure so that it is reported against every
// field that needed it.
type loader struct {
	reader     client.Reader
	namespace  string
	secrets    map[string]*corev1.Secret
	configMaps map[string]*corev1.ConfigMap
	errs       map[string]error
	versions   map[string]string
}

func (l *loader) secret(ctx context.Context, name string) (*corev1.Secret, error) {
	key := Key(ReferenceKindSecret, name)
	if err, ok := l.errs[key]; ok {
		return nil, err
	}

	if secret, ok := l.secrets[name]; ok {
		return secret, nil
	}

	secret := &corev1.Secret{}
	if err := l.reader.Get(ctx, types.NamespacedName{Namespace: l.namespace, Name: name}, secret); err != nil {
		readErr := l.readError(ReferenceKindSecret, name, err)
		l.errs[key] = readErr

		return nil, readErr
	}

	l.secrets[name] = secret
	l.versions[key] = secret.ResourceVersion

	return secret, nil
}

func (l *loader) configMap(ctx context.Context, name string) (*corev1.ConfigMap, error) {
	key := Key(ReferenceKindConfigMap, name)
	if err, ok := l.errs[key]; ok {
		return nil, err
	}

	if configMap, ok := l.configMaps[name]; ok {
		return configMap, nil
	}

	configMap := &corev1.ConfigMap{}
	if err := l.reader.Get(ctx, types.NamespacedName{Namespace: l.namespace, Name: name}, configMap); err != nil {
		readErr := l.readError(ReferenceKindConfigMap, name, err)
		l.errs[key] = readErr

		return nil, readErr
	}

	l.configMaps[name] = configMap
	l.versions[key] = configMap.ResourceVersion

	return configMap, nil
}

// readError classifies a failed read: a missing object waits for the watch on it,
// anything else is the operator's to retry.
func (l *loader) readError(kind ReferenceKind, name string, err error) *Error {
	if apierrors.IsNotFound(err) {
		reason := ReasonSecretNotFound
		if kind == ReferenceKindConfigMap {
			reason = ReasonConfigMapNotFound
		}

		return &Error{
			Reason:  reason,
			Message: fmt.Sprintf("%s %q not found in namespace %q", kind, name, l.namespace),
			Outcome: ResolveOutcomeWaitForWatch,
			cause:   err,
		}
	}

	return &Error{
		Reason:  ReasonReferenceReadFailed,
		Message: fmt.Sprintf("could not read %s %q in namespace %q: %s", kind, name, l.namespace, err),
		Outcome: ResolveOutcomeRetry,
		cause:   err,
	}
}

func readMessage(err error) string {
	if apierrors.IsNotFound(err) {
		return "not found"
	}

	return err.Error()
}

func (l *loader) endpoint(ctx context.Context, spec *resourcesv1alpha1.ProviderConfigSpec) (*url.URL, error) {
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
		return nil, &Error{Reason: ReasonEndpointInvalid, Message: path + ": value is not a valid URL", Outcome: ResolveOutcomeWaitForWatch, cause: parseErr}
	case endpoint.Scheme != "http" && endpoint.Scheme != "https":
		return nil, &Error{Reason: ReasonEndpointInvalid, Message: path + ": URL scheme must be http or https", Outcome: ResolveOutcomeWaitForWatch}
	case endpoint.Host == "":
		return nil, &Error{Reason: ReasonEndpointInvalid, Message: path + ": URL has no host", Outcome: ResolveOutcomeWaitForWatch}
	}

	return endpoint, nil
}

// credential resolves the one authentication method that is set into the header name
// and value to send.
func (l *loader) credential(ctx context.Context, auth *resourcesv1alpha1.Authentication) (string, string, error) {
	if auth.Header == nil {
		return "", "", &Error{Reason: ReasonSpecInvalid, Message: "spec.auth: no authentication method set", Outcome: ResolveOutcomeWaitForWatch}
	}

	const path = "spec.auth.header"

	header := auth.Header

	value, err := l.value(ctx, path, header.Value, header.ValueFrom)
	if err != nil {
		return "", "", err
	}

	if value == "" {
		return "", "", &Error{Reason: ReasonValueEmpty, Message: path + ": credential resolved to an empty value", Outcome: ResolveOutcomeWaitForWatch}
	}

	name := header.Name
	if name == "" {
		name = DefaultHeaderName
	}

	if header.Scheme != "" {
		value = header.Scheme + " " + value
	}

	return name, value, nil
}

func (l *loader) trust(ctx context.Context, config *resourcesv1alpha1.TLSConfig) (bool, *x509.CertPool, error) {
	if config == nil {
		return false, nil, nil
	}

	if config.CASecretRef == nil {
		return config.InsecureSkipVerify, nil, nil
	}

	const path = "spec.tls.caSecretRef"

	ref := config.CASecretRef

	secret, err := l.secret(ctx, ref.Name)
	if err != nil {
		return config.InsecureSkipVerify, nil, withPath(err, path)
	}

	bundle, ok := secret.Data[ref.Key]
	if !ok {
		return config.InsecureSkipVerify, nil, &Error{
			Reason:  ReasonKeyNotFound,
			Message: fmt.Sprintf("%s: Secret %q has no key %q", path, ref.Name, ref.Key),
			Outcome: ResolveOutcomeWaitForWatch,
		}
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(bundle) {
		return config.InsecureSkipVerify, nil, &Error{
			Reason:  ReasonCABundleInvalid,
			Message: fmt.Sprintf("%s: Secret %q key %q holds no PEM certificate", path, ref.Name, ref.Key),
			Outcome: ResolveOutcomeWaitForWatch,
		}
	}

	return config.InsecureSkipVerify, pool, nil
}

// value returns a value given inline or sourced from a Secret or ConfigMap key, with
// path naming the field for the message on failure.
//
// Surrounding whitespace is trimmed: neither a credential nor a URL can carry any,
// and the trailing newline a file-sourced Secret key ends up with would otherwise
// make every request unsendable.
func (l *loader) value(
	ctx context.Context,
	path string,
	inline string,
	from *resourcesv1alpha1.ValueSource,
) (string, error) {
	if from == nil {
		if inline == "" {
			return "", &Error{Reason: ReasonSpecInvalid, Message: path + ": set exactly one of value or valueFrom", Outcome: ResolveOutcomeWaitForWatch}
		}

		return strings.TrimSpace(inline), nil
	}

	switch {
	case from.SecretKeyRef != nil:
		ref := from.SecretKeyRef
		fieldPath := path + ".valueFrom.secretKeyRef"

		secret, err := l.secret(ctx, ref.Name)
		if err != nil {
			return "", withPath(err, fieldPath)
		}

		value, ok := secret.Data[ref.Key]
		if !ok {
			return "", &Error{
				Reason:  ReasonKeyNotFound,
				Message: fmt.Sprintf("%s: Secret %q has no key %q", fieldPath, ref.Name, ref.Key),
				Outcome: ResolveOutcomeWaitForWatch,
			}
		}

		return strings.TrimSpace(string(value)), nil

	case from.ConfigMapKeyRef != nil:
		ref := from.ConfigMapKeyRef
		fieldPath := path + ".valueFrom.configMapKeyRef"

		configMap, err := l.configMap(ctx, ref.Name)
		if err != nil {
			return "", withPath(err, fieldPath)
		}

		value, ok := configMap.Data[ref.Key]
		if !ok {
			return "", &Error{
				Reason:  ReasonKeyNotFound,
				Message: fmt.Sprintf("%s: ConfigMap %q has no key %q", fieldPath, ref.Name, ref.Key),
				Outcome: ResolveOutcomeWaitForWatch,
			}
		}

		return strings.TrimSpace(value), nil

	default:
		return "", &Error{
			Reason:  ReasonSpecInvalid,
			Message: path + ".valueFrom: set exactly one of secretKeyRef or configMapKeyRef",
			Outcome: ResolveOutcomeWaitForWatch,
		}
	}
}

func withPath(err error, path string) error {
	var resolveErr *Error
	if errors.As(err, &resolveErr) {
		return resolveErr.at(path)
	}

	return err
}
