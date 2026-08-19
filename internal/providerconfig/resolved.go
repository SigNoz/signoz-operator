package providerconfig

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/url"
)

// ResolvedProviderConfig is a provider config's spec with every reference read:
// everything needed to send an authenticated request to SigNoz.
type ResolvedProviderConfig struct {
	Endpoint   *url.URL
	HeaderName string

	// HeaderValue is the credential with the scheme prefix applied. It is credential
	// material: it must not reach a log line, an event, or a status.
	HeaderValue string

	InsecureSkipVerify bool

	// CAPool is the bundle to verify the server against, nil for the system pool.
	CAPool *x509.CertPool
}

// String redacts the credential, so formatting a resolved config into a log line
// cannot leak one. The receiver is a value so that both the value and pointer forms
// redact.
func (c ResolvedProviderConfig) String() string {
	endpoint := ""
	if c.Endpoint != nil {
		endpoint = c.Endpoint.Redacted()
	}

	return fmt.Sprintf("ResolvedProviderConfig{endpoint: %s, header: %s, value: [REDACTED]}", endpoint, c.HeaderName)
}

func (c ResolvedProviderConfig) SetAuthHeader(h http.Header) {
	h.Set(c.HeaderName, c.HeaderValue)
}

// TLSClientConfig is the TLS configuration the resolved config asks for, or nil
// when it asks for none and the transport default applies.
func (c ResolvedProviderConfig) TLSClientConfig() *tls.Config {
	if !c.InsecureSkipVerify && c.CAPool == nil {
		return nil
	}

	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: c.InsecureSkipVerify, //nolint:gosec // opt-in via spec.tls.insecureSkipVerify
		RootCAs:            c.CAPool,
	}
}
