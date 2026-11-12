package jsonbody

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSet(t *testing.T) {
	testCases := []struct {
		name          string
		body          string
		path          string
		value         string
		expected      string
		expectedError string
	}{
		{
			name:     "TopLevelKey_Added",
			body:     `{"name":"signoz.io"}`,
			path:     "token",
			value:    "t0p",
			expected: `{"name":"signoz.io","token":"t0p"}`,
		},
		{
			name:     "NestedKey_Replaced",
			body:     `{"config":{"kind":"google","spec":{"clientId":"id","clientSecret":""}}}`,
			path:     "config.spec.clientSecret",
			value:    "s3cret",
			expected: `{"config":{"kind":"google","spec":{"clientId":"id","clientSecret":"s3cret"}}}`,
		},
		{
			name:     "MissingObjects_Created",
			body:     `{"kind":"oidc"}`,
			path:     "config.spec.issuer",
			value:    "https://issuer.example",
			expected: `{"kind":"oidc","config":{"spec":{"issuer":"https://issuer.example"}}}`,
		},
		{
			name:     "NullOnPath_ReplacedByObject",
			body:     `{"auth":null}`,
			path:     "auth.password",
			value:    "pw",
			expected: `{"auth":{"password":"pw"}}`,
		},
		{
			name:     "ListIndex_ElementSet",
			body:     `{"receivers":[{"url":"a"},{"url":"b"}]}`,
			path:     "receivers.1.url",
			value:    "https://hooks.example",
			expected: `{"receivers":[{"url":"a"},{"url":"https://hooks.example"}]}`,
		},
		{
			name:     "LargeNumberElsewhere_Preserved",
			body:     `{"id":9007199254740993,"key":"old"}`,
			path:     "key",
			value:    "new",
			expected: `{"id":9007199254740993,"key":"new"}`,
		},
		{
			name:          "ListIndex_OutOfRange",
			body:          `{"items":[{"v":"x"}]}`,
			path:          "items.3.v",
			value:         "hidden-one",
			expectedError: `"3" is not an index into a list of 1`,
		},
		{
			name:          "ListIndex_NotANumber",
			body:          `{"entries":[]}`,
			path:          "entries.first",
			value:         "hidden-two",
			expectedError: `"first" is not an index`,
		},
		{
			name:          "ScalarOnPath_Rejected",
			body:          `{"enabled":true}`,
			path:          "enabled.flag",
			value:         "hidden-three",
			expectedError: "not an object or a list",
		},
		{
			name:          "MalformedBody_Rejected",
			body:          `{"open":`,
			path:          "open",
			value:         "hidden-four",
			expectedError: "unexpected EOF",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			out, err := Set(json.RawMessage(testCase.body), testCase.path, testCase.value)

			if testCase.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.expectedError)
				assert.NotContains(t, err.Error(), testCase.value)

				return
			}

			require.NoError(t, err)
			assert.JSONEq(t, testCase.expected, string(out))
			assert.Contains(t, string(out), testCase.value)
		})
	}
}
