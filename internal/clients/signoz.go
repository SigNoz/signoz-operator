package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/SigNoz/signoz-operator/internal/build"
	"github.com/SigNoz/signoz-operator/internal/providerconfig"
)

var userAgent = "signoz-operator/" + build.Version

// SigNoz sends authenticated requests to a SigNoz backend. Request URLs are
// relative — a path and optional query — resolved against the backend's
// endpoint.
type SigNoz interface {
	// Do sends one request as-is and returns the raw response.
	Do(context.Context, *http.Request) (*http.Response, error)

	// Exchange sends one JSON request and returns the status code and the
	// response parsed as JSON, so a caller always works with one shape: an empty
	// body — a 204 — is an empty map, and a non-JSON body comes back as
	// {"response": <snippet>}. An HTTP status is never an error here.
	Exchange(ctx context.Context, method, path string, body []byte) (int, map[string]any, error)
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

func (c *client) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	req = req.Clone(ctx)

	target := c.resolved.Endpoint.JoinPath(req.URL.Path)
	target.RawQuery = req.URL.RawQuery
	req.URL = target

	c.resolved.SetAuthHeader(req.Header)
	req.Header.Set("User-Agent", userAgent)

	return c.http.Do(req)
}

func (c *client) Exchange(ctx context.Context, method, path string, body []byte) (int, map[string]any, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, path, reader)
	if err != nil {
		return 0, nil, err
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.Do(ctx, req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, err
	}

	if len(data) == 0 {
		return resp.StatusCode, map[string]any{}, nil
	}

	// A non-JSON body — a proxy's error page — is wrapped rather than dropped,
	// so classification still has something quotable.
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return resp.StatusCode, map[string]any{"response": snippet(data)}, nil
	}

	return resp.StatusCode, result, nil
}

// snippet returns a short, single-line view of a non-JSON response body, so it
// travels inside the wrapper object without bloating a condition message.
func snippet(body []byte) string {
	const max = 200

	trimmed := body
	if len(trimmed) > max {
		trimmed = trimmed[:max]
	}

	compact := make([]byte, 0, len(trimmed))
	for _, b := range trimmed {
		if b == '\n' || b == '\r' || b == '\t' {
			b = ' '
		}

		compact = append(compact, b)
	}

	return string(compact)
}
