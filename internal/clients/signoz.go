package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"

	"github.com/SigNoz/signoz-operator/internal/build"
	"github.com/SigNoz/signoz-operator/internal/errors"
	"github.com/SigNoz/signoz-operator/internal/providerconfig"
)

// maxResponseBody caps what one response may pull into operator memory. It sits
// far above any legitimate SigNoz payload, so only a runaway or hostile endpoint
// reaches it.
const maxResponseBody = 64 << 20

var userAgent = "signoz-operator/" + build.Version

type SigNoz interface {
	// Exchange sends one JSON request and returns the status code and the raw
	// response body, which is always valid JSON or nil: an empty body — a 204 —
	// is nil, a non-JSON body on an error status comes back as
	// {"response": <snippet>}, and a non-JSON body on a success status — a
	// proxy's login page answering 200 — is an error. An HTTP error status is
	// never an error here; a body past maxResponseBody is.
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

func (c *client) Exchange(ctx context.Context, method, path string, body []byte) (int, json.RawMessage, error) {
	target, err := c.target(path)
	if err != nil {
		return 0, nil, errors.Wrap(err, errors.ReasonInvalidInput, "could not build the request URL")
	}

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return 0, nil, errors.Wrap(err, errors.ReasonInvalidInput, "could not build the request")
	}

	c.resolved.SetAuthHeader(req.Header)
	req.Header.Set("User-Agent", userAgent)

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, errors.Wrap(err, errors.ReasonUnreachable, "could not reach SigNoz")
	}
	defer func() { _ = resp.Body.Close() }()

	// One byte past the cap distinguishes an oversized body from one that just
	// fills it, so nothing is truncated in silence.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody+1))
	if err != nil {
		return 0, nil, errors.Wrap(err, errors.ReasonUnreachable, "could not read the response body")
	}

	if len(data) > maxResponseBody {
		return 0, nil, errors.Newf(errors.ReasonUnreachable, "the server returned a response body over the %d byte limit", maxResponseBody)
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
	// Marshalling a map[string]string cannot fail.
	wrapped, _ := json.Marshal(map[string]string{"response": snippet(data)})

	return resp.StatusCode, wrapped, nil
}

// target resolves path, which may carry a query string, against the endpoint.
// Handing the result to the request as a URL string is what keeps the request
// line origin-form: String reinstates the leading slash JoinPath drops when the
// endpoint has no path of its own.
func (c *client) target(path string) (string, error) {
	ref, err := url.Parse(path)
	if err != nil {
		return "", err
	}

	target := c.resolved.Endpoint.JoinPath(ref.EscapedPath())
	target.RawQuery = ref.RawQuery

	return target.String(), nil
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
