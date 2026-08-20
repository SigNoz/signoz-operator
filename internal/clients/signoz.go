// Package clients holds the transport clients adapters send requests through.
package clients

import (
	"context"
	"net/http"

	"github.com/SigNoz/signoz-operator/internal/providerconfig"
)

// SigNoz sends one authenticated request to a SigNoz backend. The request's
// URL is relative — a path and optional query — resolved against the backend's
// endpoint.
type SigNoz interface {
	Do(context.Context, *http.Request) (*http.Response, error)
}

// client implements SigNoz against one backend. It is built per reconcile from
// a resolved provider config spec and holds credential material through it, so
// a client must not be logged.
type client struct {
	resolved *providerconfig.ResolvedProviderConfigSpec
	http     *http.Client
}

// New builds a client for the given resolved provider config. The HTTP client
// has no retry layer: a POST must never be retried automatically
// (docs/idempotency.md), so the reconcile loop drives every retry itself.
func New(resolved *providerconfig.ResolvedProviderConfigSpec) SigNoz {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if tlsConfig := resolved.TLSClientConfig(); tlsConfig != nil {
		transport.TLSClientConfig = tlsConfig
	}

	return &client{
		resolved: resolved,
		http:     &http.Client{Transport: transport},
	}
}

// Do resolves the request's relative URL against the endpoint (joined, so a
// base path on the endpoint is kept), attaches the credential, and sends the
// request. A transport failure (dial, timeout, TLS) is returned as the error;
// an HTTP status is never an error here — the caller classifies it against
// docs/core-status.md.
func (c *client) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	req = req.Clone(ctx)

	target := c.resolved.Endpoint.JoinPath(req.URL.Path)
	target.RawQuery = req.URL.RawQuery
	req.URL = target

	c.resolved.SetAuthHeader(req.Header)
	req.Header.Set("User-Agent", "signoz-operator")

	return c.http.Do(req)
}
