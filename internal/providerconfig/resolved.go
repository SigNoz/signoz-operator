package providerconfig

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/url"
)

// ResolvedProviderConfigSpec is a ProviderConfigSpec with every reference read field for field.
type ResolvedProviderConfigSpec struct {
	Endpoint *url.URL
	Auth     ResolvedAuthentication
	TLS      *ResolvedTLSConfig
}

type ResolvedAuthentication struct {
	Header *ResolvedHeaderAuthentication
}

type ResolvedHeaderAuthentication struct {
	Name   string
	Scheme string
	Value  string
}

type ResolvedTLSConfig struct {
	InsecureSkipVerify bool
	CAPool             *x509.CertPool
}

func (a ResolvedHeaderAuthentication) String() string {
	return fmt.Sprintf("ResolvedHeaderAuthentication{name: %s, value: [REDACTED]}", a.Name)
}

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
