package jsonbody

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtract(t *testing.T) {
	type identity struct {
		Name string `json:"name"`
	}

	testCases := []struct {
		name         string
		raw          string
		expectedName string
	}{
		{
			name:         "DeclaredField_Returned",
			raw:          `{"name":"x","extra":1}`,
			expectedName: "x",
		},
		{
			name:         "FieldAbsent_ZeroValue",
			raw:          `{"extra":1}`,
			expectedName: "",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			value, err := Extract[identity](testCase.raw)
			require.NoError(t, err)

			assert.Equal(t, testCase.expectedName, value.Name)
		})
	}
}

func TestExtractInvalidJSON(t *testing.T) {
	_, err := Extract[struct{}](`{"name":`)

	assert.ErrorContains(t, err, "not valid JSON")
}

func TestExtractFields(t *testing.T) {
	testCases := []struct {
		name         string
		raw          string
		fields       []string
		expectedBody string
	}{
		{
			name:         "FieldsPresent_Projected",
			raw:          `{"kind":"graph","meta":{"a":1},"extra":true}`,
			fields:       []string{"kind", "meta"},
			expectedBody: `{"kind":"graph","meta":{"a":1}}`,
		},
		{
			name:         "FieldAbsent_Skipped",
			raw:          `{"name":"x"}`,
			fields:       []string{"name", "tags"},
			expectedBody: `{"name":"x"}`,
		},
		{
			name:         "NoFieldsPresent_Nil",
			raw:          `{"extra":true}`,
			fields:       []string{"badge", "links"},
			expectedBody: "",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			body, err := ExtractFields(json.RawMessage(testCase.raw), testCase.fields)
			require.NoError(t, err)

			if testCase.expectedBody == "" {
				assert.Nil(t, body)
			} else {
				assert.JSONEq(t, testCase.expectedBody, string(body))
			}
		})
	}
}

func TestExtractString(t *testing.T) {
	testCases := []struct {
		name          string
		raw           string
		path          string
		expectedValue string
		expectedErr   string
	}{
		{
			name:          "NestedPath_Returned",
			raw:           `{"data":{"id":"srv-1"}}`,
			path:          "data.id",
			expectedValue: "srv-1",
		},
		{
			name:        "PathAbsent_Error",
			raw:         `{"data":{}}`,
			path:        "data.token",
			expectedErr: "no value at data.token",
		},
		{
			name:        "ValueEmpty_Error",
			raw:         `{"data":{"uid":""}}`,
			path:        "data.uid",
			expectedErr: "no value at data.uid",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			value, err := ExtractString(json.RawMessage(testCase.raw), testCase.path)

			if testCase.expectedErr != "" {
				assert.ErrorContains(t, err, testCase.expectedErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.expectedValue, value)
		})
	}
}
