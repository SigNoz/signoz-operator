package jsonbody

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHash(t *testing.T) {
	testCases := []struct {
		name          string
		body          string
		other         string
		expectedEqual bool
	}{
		{
			name:          "ReformattedBody_SameHash",
			body:          `{"a":1,"b":[1,2]}`,
			other:         "{\n  \"b\": [1, 2],\n  \"a\": 1\n}",
			expectedEqual: true,
		},
		{
			name:          "DifferentContent_DifferentHash",
			body:          `{"a":1}`,
			other:         `{"a":2}`,
			expectedEqual: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			bodyHash, err := Hash(json.RawMessage(testCase.body))
			require.NoError(t, err)

			otherHash, err := Hash(json.RawMessage(testCase.other))
			require.NoError(t, err)

			assert.Equal(t, testCase.expectedEqual, bodyHash == otherHash)
		})
	}
}

func TestHashInvalidJSON(t *testing.T) {
	_, err := Hash(json.RawMessage(`{"a":`))

	assert.Error(t, err)
}
