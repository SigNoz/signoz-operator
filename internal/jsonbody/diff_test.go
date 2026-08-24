package jsonbody

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiff(t *testing.T) {
	testCases := []struct {
		name          string
		a             string
		b             string
		expectedPaths []string
	}{
		{
			name:          "IdenticalValues_NoDiff",
			a:             `{"name":"x","spec":{"panels":{"p1":{"kind":"graph"}}}}`,
			b:             `{"name":"x","spec":{"panels":{"p1":{"kind":"graph"}}}}`,
			expectedPaths: nil,
		},
		{
			name:          "AddedKey_PathReported",
			a:             `{"kind":"graph"}`,
			b:             `{"kind":"graph","id":"srv-1"}`,
			expectedPaths: []string{"id"},
		},
		{
			name:          "MissingKey_PathReported",
			a:             `{"ttl":5}`,
			b:             `{}`,
			expectedPaths: []string{"ttl"},
		},
		{
			name:          "DeepScalarChanged_PathReported",
			a:             `{"spec":{"layouts":[{"items":[{"ref":"a"},{"ref":"b"}]}]}}`,
			b:             `{"spec":{"layouts":[{"items":[{"ref":"a"},{"ref":"c"}]}]}}`,
			expectedPaths: []string{"spec.layouts.0.items.1.ref"},
		},
		{
			name:          "NullVsAbsent_Diverges",
			a:             `{"a":null}`,
			b:             `{}`,
			expectedPaths: []string{"a"},
		},
		{
			name:          "EmptyArrayVsNull_Diverges",
			a:             `{"a":[]}`,
			b:             `{"a":null}`,
			expectedPaths: []string{"a"},
		},
		{
			name:          "ArrayElementMissing_PathReported",
			a:             `{"a":[1,2]}`,
			b:             `{"a":[1]}`,
			expectedPaths: []string{"a.1"},
		},
		{
			name:          "TypeMismatch_PathReported",
			a:             `{"a":{"b":1}}`,
			b:             `{"a":"x"}`,
			expectedPaths: []string{"a"},
		},
		{
			name:          "MultipleDivergences_AllReported",
			a:             `{"a":1,"b":2}`,
			b:             `{"a":9,"b":8}`,
			expectedPaths: []string{"a", "b"},
		},
		{
			name:          "RootScalarChanged_RootPath",
			a:             `1`,
			b:             `2`,
			expectedPaths: []string{""},
		},
		{
			name:          "IntegerAndFloatForms_Equal",
			a:             `{"a":1}`,
			b:             `{"a":1.0}`,
			expectedPaths: nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			paths := Diff(decode(t, testCase.a), decode(t, testCase.b))

			assert.Equal(t, testCase.expectedPaths, paths)
			assert.Equal(t, len(testCase.expectedPaths) == 0, Equal(decode(t, testCase.a), decode(t, testCase.b)))
		})
	}
}

func TestDiffWithFields(t *testing.T) {
	testCases := []struct {
		name              string
		desired           string
		remote            string
		updatable         []string
		immutable         []string
		expectedUpdatable []string
		expectedImmutable []string
	}{
		{
			name:              "RemoteMatchesDesired_NoDiff",
			desired:           `{"title":"latency","layout":{"rows":1}}`,
			remote:            `{"title":"latency","layout":{"rows":1}}`,
			updatable:         []string{"title", "layout"},
			expectedUpdatable: nil,
		},
		{
			name:              "ServerAddedNestedKeys_Diverges",
			desired:           `{"spec":{"panels":{"p1":{"kind":"graph"}}}}`,
			remote:            `{"id":"srv-1","spec":{"panels":{"p1":{"kind":"graph","uuid":"u1"}},"variables":[]}}`,
			updatable:         []string{"spec"},
			expectedUpdatable: []string{"spec"},
		},
		{
			name:              "RemoteEditedNestedValue_Diverges",
			desired:           `{"config":{"mode":"fast"}}`,
			remote:            `{"config":{"mode":"slow"}}`,
			updatable:         []string{"config"},
			expectedUpdatable: []string{"config"},
		},
		{
			name:              "FieldAbsentInDesired_Skipped",
			desired:           `{"alias":"x"}`,
			remote:            `{"alias":"y","labels":["a"]}`,
			updatable:         []string{"alias", "labels"},
			expectedUpdatable: []string{"alias"},
		},
		{
			name:              "UpdatableFieldAbsentInRemote_Diverges",
			desired:           `{"owner":"x"}`,
			remote:            `{}`,
			updatable:         []string{"owner"},
			expectedUpdatable: []string{"owner"},
		},
		{
			name:              "ImmutableFieldAbsentInRemote_Skipped",
			desired:           `{"email":"a@b.c"}`,
			remote:            `{}`,
			immutable:         []string{"email"},
			expectedImmutable: nil,
		},
		{
			name:              "ImmutableFieldChanged_Reported",
			desired:           `{"region":"eu"}`,
			remote:            `{"region":"us"}`,
			immutable:         []string{"region"},
			expectedImmutable: []string{"region"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			diff := DiffWithFields(json.RawMessage(testCase.desired), json.RawMessage(testCase.remote), testCase.updatable, testCase.immutable)

			assert.Equal(t, testCase.expectedUpdatable, diff.Updatable)
			assert.Equal(t, testCase.expectedImmutable, diff.Immutable)
		})
	}
}

func decode(t *testing.T, raw string) any {
	t.Helper()

	var value any
	require.NoError(t, json.Unmarshal([]byte(raw), &value))

	return value
}
