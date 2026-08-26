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

// maxResponseBody caps what one response may pull into operator memory.
const maxResponseBody = 64 << 20

var userAgent = "signoz-operator/" + build.Version

type SigNoz interface {
	// Exchange sends one JSON request and returns the status code and the raw
	// response body. The body is always valid JSON or nil: an empty body is nil,
	// a non-JSON body on an error status comes back as {"response": <snippet>},
	// and a non-JSON body on a success status is an error. An HTTP error status
	// is not an error here; a body past maxResponseBody is.
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
	ref, err := url.Parse(path)
	if err != nil {
		return 0, nil, errors.Wrap(err, errors.ReasonInvalidInput, "could not build the request URL")
	}

	// String reinstates the leading slash JoinPath drops when the endpoint has no
	// path of its own, keeping the request line origin-form.
	target := c.resolved.Endpoint.JoinPath(ref.EscapedPath())
	target.RawQuery = ref.RawQuery

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, target.String(), reader)
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

	// One byte past the cap tells an oversized body from one that just fills it.
	limited := io.LimitReader(resp.Body, maxResponseBody+1)

	var seen bytes.Buffer

	var data json.RawMessage

	decoder := json.NewDecoder(io.TeeReader(limited, &seen))

	// Content after the value leaves the body no more JSON than a syntax error does.
	valid := decoder.Decode(&data) == nil && !decoder.More()

	// The decoder stops at the end of the value, or at the first byte it cannot
	// use. Draining the rest gives the snippet the whole body and reports a
	// failed read as the transport failure it is.
	if _, err := io.Copy(&seen, limited); err != nil {
		return 0, nil, errors.Wrap(err, errors.ReasonUnreachable, "could not read the response body")
	}

	if seen.Len() > maxResponseBody {
		return 0, nil, errors.Newf(errors.ReasonUnreachable, "the server returned a response body over the %d byte limit", maxResponseBody)
	}

	if seen.Len() == 0 {
		return resp.StatusCode, nil, nil
	}

	if valid {
		return resp.StatusCode, data, nil
	}

	// A success status with a non-JSON body means something between the operator
	// and SigNoz answered.
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return 0, nil, errors.Newf(errors.ReasonUnreachable, "the server returned a success status (HTTP %d) with a non-JSON body: %s", resp.StatusCode, snippet(seen.Bytes()))
	}

	// Wrapped rather than dropped, so classification still has something quotable.
	// Marshalling a map[string]string cannot fail.
	wrapped, _ := json.Marshal(map[string]string{"response": snippet(seen.Bytes())})

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
