package adapters

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SigNoz/signoz-operator/internal/clients"
	"github.com/SigNoz/signoz-operator/internal/errors"
	"github.com/SigNoz/signoz-operator/internal/providerconfig"
	"github.com/SigNoz/signoz-operator/internal/resources/resourcestest"
)

const (
	nameField    = "name"
	prodIdentity = "prod"
)

func TestFindByField(t *testing.T) {
	testCases := []struct {
		name        string
		field       string
		identity    string
		status      int
		body        string
		expectedIDs []string
		expectedErr string
	}{
		{
			name:        "NameMatches_OneID",
			field:       nameField,
			identity:    prodIdentity,
			status:      http.StatusOK,
			body:        `{"status":"success","data":[{"id":"1","name":"dev"},{"id":"2","name":"prod"}]}`,
			expectedIDs: []string{"2"},
		},
		{
			name:     "NoEntryMatches_NoID",
			field:    nameField,
			identity: prodIdentity,
			status:   http.StatusOK,
			body:     `{"status":"success","data":[{"id":"1","name":"dev"}]}`,
		},
		{
			name:        "TwoEntriesShareTheName_BothIDs",
			field:       nameField,
			identity:    prodIdentity,
			status:      http.StatusOK,
			body:        `{"status":"success","data":[{"id":"1","name":"prod"},{"id":"2","name":"prod"}]}`,
			expectedIDs: []string{"1", "2"},
		},
		{
			name:        "IdentityInAnotherField_OneID",
			field:       "alert",
			identity:    "high latency",
			status:      http.StatusOK,
			body:        `{"status":"success","data":[{"id":"1","alert":"high latency"},{"id":"2","alert":"low disk"}]}`,
			expectedIDs: []string{"1"},
		},
		{
			name:     "NameMatchIsCaseSensitive_NoID",
			field:    nameField,
			identity: prodIdentity,
			status:   http.StatusOK,
			body:     `{"status":"success","data":[{"id":"1","name":"Prod"}]}`,
		},
		{
			name:        "EmptyList_NoID",
			field:       nameField,
			identity:    prodIdentity,
			status:      http.StatusOK,
			body:        `{"status":"success","data":[]}`,
			expectedIDs: nil,
		},
		{
			name:        "ResponseCarriesNoData_Error",
			field:       nameField,
			identity:    prodIdentity,
			status:      http.StatusOK,
			body:        `{"status":"success"}`,
			expectedErr: "response carries no data list",
		},
		{
			name:        "DataIsNotAList_Error",
			field:       nameField,
			identity:    prodIdentity,
			status:      http.StatusOK,
			body:        `{"status":"success","data":{"id":"1","name":"prod"}}`,
			expectedErr: "response carries no data list",
		},
		{
			name:        "MatchingEntryCarriesNoID_Error",
			field:       nameField,
			identity:    prodIdentity,
			status:      http.StatusOK,
			body:        `{"status":"success","data":[{"name":"prod"}]}`,
			expectedErr: "list entry carries no id",
		},
		{
			name:        "ServerRejectsTheRequest_Error",
			field:       nameField,
			identity:    prodIdentity,
			status:      http.StatusForbidden,
			body:        `{"error":"no access"}`,
			expectedErr: "the server rejected the credential (HTTP 403)",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var requested *http.Request

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requested = r

				w.WriteHeader(testCase.status)
				_, _ = w.Write([]byte(testCase.body))
			}))
			defer server.Close()

			endpoint, err := url.Parse(server.URL)
			require.NoError(t, err)

			obj := resourcestest.NewMockObject(t)
			obj.EXPECT().Identity().Return(testCase.identity, nil)

			c := clients.New(&providerconfig.ResolvedProviderConfigSpec{Endpoint: endpoint})

			matches, err := findByField(context.Background(), c, obj, "/api/v1/things", testCase.field)

			require.Equal(t, http.MethodGet, requested.Method)
			assert.Equal(t, "/api/v1/things", requested.URL.Path)

			if testCase.expectedErr != "" {
				assert.ErrorContains(t, err, testCase.expectedErr)
				return
			}

			require.NoError(t, err)
			require.Len(t, matches, len(testCase.expectedIDs))

			for i, expectedID := range testCase.expectedIDs {
				require.NotNil(t, matches[i].ID)
				assert.Equal(t, expectedID, *matches[i].ID)
			}
		})
	}
}

func TestFindByFieldIdentityError(t *testing.T) {
	obj := resourcestest.NewMockObject(t)
	obj.EXPECT().Identity().Return("", errors.New(errors.ReasonInvalidInput, "objectTemplate: not valid JSON"))

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("Find reached the server without an identity")
	}))
	defer server.Close()

	endpoint, err := url.Parse(server.URL)
	require.NoError(t, err)

	c := clients.New(&providerconfig.ResolvedProviderConfigSpec{Endpoint: endpoint})

	matches, err := findByField(context.Background(), c, obj, "/api/v1/things", nameField)

	assert.Nil(t, matches)
	assert.ErrorContains(t, err, "objectTemplate: not valid JSON")
}
