package clients

import (
	"context"
	"net/http"

	"github.com/SigNoz/signoz-operator/internal/build"
	"github.com/SigNoz/signoz-operator/internal/providerconfig"
)

var userAgent = "signoz-operator/" + build.Version

// SigNoz sends one authenticated request to a SigNoz backend. The request's
// URL is relative — a path and optional query — resolved against the backend's
// endpoint.
type SigNoz interface {
	Do(context.Context, *http.Request) (*http.Response, error)
}

// client holds credential material through its resolved provider config, so it
// must not be logged.
type client struct {
	resolved *providerconfig.ResolvedProviderConfigSpec
	http     *http.Client
}

// The HTTP client has no retry layer: a POST must never be retried
// automatically (docs/idempotency.md), so the reconcile loop drives every
// retry itself.
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
// request. Only a transport failure is an error; the caller classifies HTTP
// statuses.
func (c *client) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	req = req.Clone(ctx)

	target := c.resolved.Endpoint.JoinPath(req.URL.Path)
	target.RawQuery = req.URL.RawQuery
	req.URL = target

	c.resolved.SetAuthHeader(req.Header)
	req.Header.Set("User-Agent", userAgent)

	return c.http.Do(req)
}
