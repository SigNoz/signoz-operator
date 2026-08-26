package clients

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SigNoz/signoz-operator/internal/build"
	"github.com/SigNoz/signoz-operator/internal/errors"
	"github.com/SigNoz/signoz-operator/internal/providerconfig"
)

func TestExchange(t *testing.T) {
	testCases := []struct {
		name         string
		status       int
		body         string
		expectedBody string
		expectedErr  string
	}{
		{
			name:         "EmptyBody_NilMessage",
			status:       http.StatusNoContent,
			body:         "",
			expectedBody: "",
		},
		{
			name:         "ValidJSON_ReturnedRaw",
			status:       http.StatusOK,
			body:         `{"data":{"id":"1"}}`,
			expectedBody: `{"data":{"id":"1"}}`,
		},
		{
			name:        "SuccessStatusNonJSONBody_Error",
			status:      http.StatusOK,
			body:        "<html>login page</html>",
			expectedErr: "non-JSON body",
		},
		{
			name:         "ErrorStatusNonJSONBody_SnippetWrapped",
			status:       http.StatusBadGateway,
			body:         "<html>bad gateway</html>",
			expectedBody: `{"response":"<html>bad gateway</html>"}`,
		},
		{
			name:         "ErrorStatusOversizedNonJSONBody_SnippetTruncatedAndFlattened",
			status:       http.StatusServiceUnavailable,
			body:         "upstream connect error\n\t" + strings.Repeat("x", 300),
			expectedBody: `{"response":"upstream connect error  ` + strings.Repeat("x", 176) + `"}`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(testCase.status)
				_, _ = w.Write([]byte(testCase.body))
			}))
			defer server.Close()

			endpoint, err := url.Parse(server.URL)
			require.NoError(t, err)

			status, body, err := New(&providerconfig.ResolvedProviderConfigSpec{Endpoint: endpoint}).Exchange(context.Background(), http.MethodGet, "/api/v2/things", nil)

			if testCase.expectedErr != "" {
				assert.ErrorContains(t, err, testCase.expectedErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.status, status)

			if testCase.expectedBody == "" {
				assert.Empty(t, body)
			} else {
				assert.JSONEq(t, testCase.expectedBody, string(body))
			}
		})
	}
}

// The request line the server sees is the whole contract of the URL building:
// origin-form, the endpoint's own path in front, query and escaping intact.
func TestExchangeRequestLine(t *testing.T) {
	testCases := []struct {
		name             string
		basePath         string
		path             string
		expectedObserved string
	}{
		{
			name:             "EndpointWithoutPath_KeepsTheLeadingSlash",
			path:             "/api/v2/widgets",
			expectedObserved: `{"uri":"/api/v2/widgets","decoded":"/api/v2/widgets"}`,
		},
		{
			name:             "EndpointWithBasePath_PrefixesRequestPath",
			basePath:         "/signoz",
			path:             "/api/v2/gadgets",
			expectedObserved: `{"uri":"/signoz/api/v2/gadgets","decoded":"/signoz/api/v2/gadgets"}`,
		},
		{
			name:             "EndpointWithTrailingSlashBasePath_PrefixesRequestPath",
			basePath:         "/signoz/",
			path:             "/api/v2/doodads",
			expectedObserved: `{"uri":"/signoz/api/v2/doodads","decoded":"/signoz/api/v2/doodads"}`,
		},
		{
			name:             "PathCarriesQueryString_QuerySurvives",
			path:             "/api/v2/trinkets?limit=10&offset=5",
			expectedObserved: `{"uri":"/api/v2/trinkets?limit=10&offset=5","decoded":"/api/v2/trinkets"}`,
		},
		{
			name:             "BasePathAndQueryString_BothSurvive",
			basePath:         "/observability",
			path:             "/api/v2/baubles?limit=10",
			expectedObserved: `{"uri":"/observability/api/v2/baubles?limit=10","decoded":"/observability/api/v2/baubles"}`,
		},
		{
			name:             "SegmentWithSpace_EscapedOnTheWire",
			path:             "/api/v2/sprockets/id with space",
			expectedObserved: `{"uri":"/api/v2/sprockets/id%20with%20space","decoded":"/api/v2/sprockets/id with space"}`,
		},
		{
			name:             "SegmentWithEscapedSlash_StaysEscapedOnTheWire",
			path:             "/api/v2/cogs/one%2Ftwo",
			expectedObserved: `{"uri":"/api/v2/cogs/one%2Ftwo","decoded":"/api/v2/cogs/one/two"}`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]string{"uri": r.RequestURI, "decoded": r.URL.Path})
			}))
			defer server.Close()

			endpoint, err := url.Parse(server.URL + testCase.basePath)
			require.NoError(t, err)

			_, body, err := New(&providerconfig.ResolvedProviderConfigSpec{Endpoint: endpoint}).Exchange(context.Background(), http.MethodGet, testCase.path, nil)
			require.NoError(t, err)

			assert.JSONEq(t, testCase.expectedObserved, string(body))
		})
	}
}

func TestExchangeHeaders(t *testing.T) {
	testCases := []struct {
		name                string
		auth                providerconfig.ResolvedAuthentication
		body                []byte
		headerName          string
		expectedAuth        string
		expectedContentType string
	}{
		{
			name:         "HeaderAuthWithoutScheme_SendsTheValueVerbatim",
			auth:         providerconfig.ResolvedAuthentication{Header: &providerconfig.ResolvedHeaderAuthentication{Name: "X-SigNoz-Key", Value: "opaque-key"}},
			headerName:   "X-SigNoz-Key",
			expectedAuth: "opaque-key",
		},
		{
			name:                "HeaderAuthWithScheme_PrefixesTheSchemeAndTypesTheBody",
			auth:                providerconfig.ResolvedAuthentication{Header: &providerconfig.ResolvedHeaderAuthentication{Name: "Authorization", Scheme: "Bearer", Value: "issued-token"}},
			body:                []byte(`{"name":"flywheel"}`),
			headerName:          "Authorization",
			expectedAuth:        "Bearer issued-token",
			expectedContentType: "application/json",
		},
		{
			name:       "NoAuth_LeavesTheHeaderUnset",
			headerName: "SIGNOZ-API-KEY",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]string{
					"credential": r.Header.Get(testCase.headerName),
					"agent":      r.Header.Get("User-Agent"),
					"type":       r.Header.Get("Content-Type"),
				})
			}))
			defer server.Close()

			endpoint, err := url.Parse(server.URL)
			require.NoError(t, err)

			resolved := &providerconfig.ResolvedProviderConfigSpec{Endpoint: endpoint, Auth: testCase.auth}

			_, body, err := New(resolved).Exchange(context.Background(), http.MethodPost, "/api/v2/ratchets", testCase.body)
			require.NoError(t, err)

			var observed map[string]string
			require.NoError(t, json.Unmarshal(body, &observed))

			assert.Equal(t, testCase.expectedAuth, observed["credential"])
			assert.Equal(t, testCase.expectedContentType, observed["type"])
			assert.Equal(t, "signoz-operator/"+build.Version, observed["agent"])
		})
	}
}

func TestExchangeTLS(t *testing.T) {
	testCases := []struct {
		name        string
		tlsFor      func(*x509.Certificate) *providerconfig.ResolvedTLSConfig
		expectedErr string
	}{
		{
			name: "InsecureSkipVerify_ReachesTheServer",
			tlsFor: func(*x509.Certificate) *providerconfig.ResolvedTLSConfig {
				return &providerconfig.ResolvedTLSConfig{InsecureSkipVerify: true}
			},
		},
		{
			name: "CAPoolCarryingTheServerCertificate_ReachesTheServer",
			tlsFor: func(certificate *x509.Certificate) *providerconfig.ResolvedTLSConfig {
				pool := x509.NewCertPool()
				pool.AddCert(certificate)

				return &providerconfig.ResolvedTLSConfig{CAPool: pool}
			},
		},
		{
			name:        "NoTLSConfig_UntrustedCertificateIsUnreachable",
			tlsFor:      func(*x509.Certificate) *providerconfig.ResolvedTLSConfig { return nil },
			expectedErr: "could not reach SigNoz",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"data":{"id":"over-tls"}}`))
			}))
			server.Config.ErrorLog = log.New(io.Discard, "", 0)

			defer server.Close()

			endpoint, err := url.Parse(server.URL)
			require.NoError(t, err)

			resolved := &providerconfig.ResolvedProviderConfigSpec{Endpoint: endpoint, TLS: testCase.tlsFor(server.Certificate())}

			status, body, err := New(resolved).Exchange(context.Background(), http.MethodGet, "/api/v2/pulleys", nil)

			if testCase.expectedErr != "" {
				assert.ErrorContains(t, err, testCase.expectedErr)
				assert.Equal(t, errors.ReasonUnreachable, errors.ReasonForError(err))

				return
			}

			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, status)
			assert.JSONEq(t, `{"data":{"id":"over-tls"}}`, string(body))
		})
	}
}

func TestExchangeFailures(t *testing.T) {
	testCases := []struct {
		name           string
		handler        http.HandlerFunc
		method         string
		path           string
		expectedErr    string
		expectedReason errors.Reason
	}{
		{
			name:           "UnparseablePath_InvalidInput",
			handler:        func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) },
			method:         http.MethodGet,
			path:           "/api/v2/levers/%zz",
			expectedErr:    "could not build the request URL",
			expectedReason: errors.ReasonInvalidInput,
		},
		{
			name:           "InvalidMethod_InvalidInput",
			handler:        func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) },
			method:         "GET ",
			path:           "/api/v2/pistons",
			expectedErr:    "net/http: invalid method",
			expectedReason: errors.ReasonInvalidInput,
		},
		{
			name: "BodyShorterThanContentLength_Unreachable",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Length", "64")
				_, _ = w.Write([]byte("truncated"))
			},
			method:         http.MethodGet,
			path:           "/api/v2/bearings",
			expectedErr:    "could not read the response body",
			expectedReason: errors.ReasonUnreachable,
		},
		{
			name: "BodyOverTheLimit_Unreachable",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				chunk := bytes.Repeat([]byte("a"), 1<<20)
				for written := 0; written <= maxResponseBody; written += len(chunk) {
					if _, err := w.Write(chunk); err != nil {
						return
					}
				}
			},
			method:         http.MethodGet,
			path:           "/api/v2/camshafts",
			expectedErr:    "over the 67108864 byte limit",
			expectedReason: errors.ReasonUnreachable,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(testCase.handler)
			server.Config.ErrorLog = log.New(io.Discard, "", 0)

			defer server.Close()

			endpoint, err := url.Parse(server.URL)
			require.NoError(t, err)

			status, body, err := New(&providerconfig.ResolvedProviderConfigSpec{Endpoint: endpoint}).Exchange(context.Background(), testCase.method, testCase.path, nil)

			assert.ErrorContains(t, err, testCase.expectedErr)
			assert.Equal(t, testCase.expectedReason, errors.ReasonForError(err))
			assert.Zero(t, status)
			assert.Nil(t, body)
		})
	}
}
