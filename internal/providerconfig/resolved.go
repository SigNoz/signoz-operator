package providerconfig

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/url"
)

// ResolvedProviderConfigSpec is a ProviderConfigSpec with every reference read,
// field for field.
type ResolvedProviderConfigSpec struct {
	Endpoint *url.URL
	Auth     ResolvedAuthentication
	TLS      *ResolvedTLSConfig
}

// ResolvedAuthentication mirrors Authentication: the one method set in the spec
// is the one non-nil here.
type ResolvedAuthentication struct {
	Header *ResolvedHeaderAuthentication
}

// ResolvedHeaderAuthentication is a HeaderAuth resolved to the header it sends,
// the default name already applied.
type ResolvedHeaderAuthentication struct {
	Name   string
	Scheme string

	// Value is the bare credential, no scheme prefix. It must not reach a log
	// line, an event, or a status.
	Value string
}

// String redacts the credential.
func (a ResolvedHeaderAuthentication) String() string {
	return fmt.Sprintf("ResolvedHeaderAuthentication{name: %s, value: [REDACTED]}", a.Name)
}

// ResolvedTLSConfig mirrors TLSConfig, the CA bundle read into a pool, nil for
// the system pool.
type ResolvedTLSConfig struct {
	InsecureSkipVerify bool
	CAPool             *x509.CertPool
}

// String redacts the credential so formatting a resolved spec cannot leak it.
// Value receiver, so both the value and pointer forms redact.
func (c ResolvedProviderConfigSpec) String() string {
	endpoint := ""
	if c.Endpoint != nil {
		endpoint = c.Endpoint.Redacted()
	}

	header := ""
	if c.Auth.Header != nil {
		header = c.Auth.Header.Name
	}

	return fmt.Sprintf("ResolvedProviderConfigSpec{endpoint: %s, header: %s, value: [REDACTED]}", endpoint, header)
}

func (c ResolvedProviderConfigSpec) SetAuthHeader(h http.Header) {
	if c.Auth.Header == nil {
		return
	}

	value := c.Auth.Header.Value

	if c.Auth.Header.Scheme != "" {
		value = c.Auth.Header.Scheme + " " + value
	}

	h.Set(c.Auth.Header.Name, value)
}

// TLSClientConfig is the TLS configuration the resolved spec asks for, nil when
// the transport default applies.
func (c ResolvedProviderConfigSpec) TLSClientConfig() *tls.Config {
	if c.TLS == nil || (!c.TLS.InsecureSkipVerify && c.TLS.CAPool == nil) {
		return nil
	}

	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: c.TLS.InsecureSkipVerify, //nolint:gosec // opt-in via spec.tls.insecureSkipVerify
		RootCAs:            c.TLS.CAPool,
	}
}
