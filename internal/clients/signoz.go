package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/SigNoz/signoz-operator/internal/build"
	"github.com/SigNoz/signoz-operator/internal/errors"
	"github.com/SigNoz/signoz-operator/internal/providerconfig"
)

var userAgent = "signoz-operator/" + build.Version

type SigNoz interface {
	// Do sends one request as-is and returns the raw response.
	Do(context.Context, *http.Request) (*http.Response, error)

	// Exchange sends one JSON request and returns the status code and the raw
	// response body, which is always valid JSON or nil: an empty body — a 204 —
	// is nil, a non-JSON body on an error status comes back as
	// {"response": <snippet>}, and a non-JSON body on a success status — a
	// proxy's login page answering 200 — is an error. An HTTP error status is
	// never an error here.
	Exchange(ctx context.Context, method, path string, body []byte) (int, json.RawMessage, error)
}

type client struct {
	resolved *providerconfig.ResolvedProviderConfigSpec
	http     *http.Client
}

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

	// JoinPath drops the leading slash when the endpoint has no path — an
	// endpoint without a trailing slash — and the request line must stay
	// origin-form.
	target := c.resolved.Endpoint.JoinPath(req.URL.Path)
	if !strings.HasPrefix(target.Path, "/") {
		target.Path = "/" + target.Path
	}

	target.RawQuery = req.URL.RawQuery
	req.URL = target

	c.resolved.SetAuthHeader(req.Header)
	req.Header.Set("User-Agent", userAgent)

	return c.http.Do(req)
}

func (c *client) Exchange(ctx context.Context, method, path string, body []byte) (int, json.RawMessage, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, path, reader)
	if err != nil {
		return 0, nil, errors.Wrap(err, errors.ReasonInvalidInput, "could not build the request")
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.Do(ctx, req)
	if err != nil {
		return 0, nil, errors.Wrap(err, errors.ReasonUnreachable, "could not reach SigNoz")
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, errors.Wrap(err, errors.ReasonUnreachable, "could not read the response body")
	}

	if len(data) == 0 {
		return resp.StatusCode, nil, nil
	}

	if json.Valid(data) {
		return resp.StatusCode, data, nil
	}

	// A success status with a non-JSON body means something between the
	// operator and SigNoz answered — a proxy's login or error page.
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return 0, nil, errors.Newf(errors.ReasonUnreachable, "the server returned a success status (HTTP %d) with a non-JSON body: %s", resp.StatusCode, snippet(data))
	}

	// A non-JSON body on an error status — a proxy's error page — is wrapped
	// rather than dropped, so classification still has something quotable.
	wrapped, err := json.Marshal(map[string]string{"response": snippet(data)})
	if err != nil {
		return 0, nil, err
	}

	return resp.StatusCode, wrapped, nil
}

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
