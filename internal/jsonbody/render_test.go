package jsonbody

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type renderSpec struct {
	Name string `json:"name"`
}

func TestRender(t *testing.T) {
	jsonSpec := "{\n  \"name\": \"raw\"\n}"

	testCases := []struct {
		name         string
		spec         *renderSpec
		jsonSpec     *string
		expectedBody string
	}{
		{
			name:         "TypedForm_Marshalled",
			spec:         &renderSpec{Name: "typed"},
			expectedBody: `{"name":"typed"}`,
		},
		{
			name:         "JSONSpecForm_Verbatim",
			jsonSpec:     &jsonSpec,
			expectedBody: jsonSpec,
		},
		{
			name:         "BothForms_SpecWins",
			spec:         &renderSpec{Name: "winner"},
			jsonSpec:     &jsonSpec,
			expectedBody: `{"name":"winner"}`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			body, err := Render(testCase.spec, testCase.jsonSpec)
			require.NoError(t, err)

			assert.Equal(t, testCase.expectedBody, string(body))
		})
	}
}

func TestRenderNeitherForm(t *testing.T) {
	_, err := Render[renderSpec](nil, nil)

	assert.ErrorContains(t, err, "exactly one of spec or jsonSpec must be set")
}
