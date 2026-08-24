package clients

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
