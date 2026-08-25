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
		x             string
		y             string
		expectedPaths []string
	}{
		{
			name:          "IdenticalValues_NoDiff",
			x:             `{"name":"x","spec":{"panels":{"p1":{"kind":"graph"}}}}`,
			y:             `{"name":"x","spec":{"panels":{"p1":{"kind":"graph"}}}}`,
			expectedPaths: nil,
		},
		{
			name:          "AddedKey_PathReported",
			x:             `{"kind":"graph"}`,
			y:             `{"kind":"graph","id":"srv-1"}`,
			expectedPaths: []string{"id"},
		},
		{
			name:          "MissingKey_PathReported",
			x:             `{"ttl":5}`,
			y:             `{}`,
			expectedPaths: []string{"ttl"},
		},
		{
			name:          "DeepScalarChanged_PathReported",
			x:             `{"spec":{"layouts":[{"items":[{"ref":"a"},{"ref":"b"}]}]}}`,
			y:             `{"spec":{"layouts":[{"items":[{"ref":"a"},{"ref":"c"}]}]}}`,
			expectedPaths: []string{"spec.layouts.0.items.1.ref"},
		},
		{
			name:          "NullVsAbsent_Diverges",
			x:             `{"a":null}`,
			y:             `{}`,
			expectedPaths: []string{"a"},
		},
		{
			name:          "EmptyArrayVsNull_Diverges",
			x:             `{"a":[]}`,
			y:             `{"a":null}`,
			expectedPaths: []string{"a"},
		},
		{
			name:          "ArrayElementMissing_PathReported",
			x:             `{"a":[1,2]}`,
			y:             `{"a":[1]}`,
			expectedPaths: []string{"a.1"},
		},
		{
			name:          "TypeMismatch_PathReported",
			x:             `{"a":{"b":1}}`,
			y:             `{"a":"x"}`,
			expectedPaths: []string{"a"},
		},
		{
			name:          "MultipleDivergences_AllReported",
			x:             `{"a":1,"b":2}`,
			y:             `{"a":9,"b":8}`,
			expectedPaths: []string{"a", "b"},
		},
		{
			name:          "RootScalarChanged_RootPath",
			x:             `1`,
			y:             `2`,
			expectedPaths: []string{""},
		},
		{
			name:          "IntegerAndFloatForms_Equal",
			x:             `{"a":1}`,
			y:             `{"a":1.0}`,
			expectedPaths: nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			paths := Diff(decode(t, testCase.x), decode(t, testCase.y))

			assert.Equal(t, testCase.expectedPaths, paths)
			assert.Equal(t, len(testCase.expectedPaths) == 0, Equal(decode(t, testCase.x), decode(t, testCase.y)))
		})
	}
}

func TestIntersect(t *testing.T) {
	testCases := []struct {
		name     string
		x        []string
		y        []string
		expected []string
	}{
		{
			name:     "ExactMatch_Returned",
			x:        []string{"id"},
			y:        []string{"id"},
			expected: []string{"id"},
		},
		{
			name:     "DiffDeeperThanField_FieldReturned",
			x:        []string{"spec.panels.p1.kind"},
			y:        []string{"spec"},
			expected: []string{"spec"},
		},
		{
			name:     "FieldDeeperThanDiff_FieldReturned",
			x:        []string{"meta"},
			y:        []string{"meta.labels"},
			expected: []string{"meta.labels"},
		},
		{
			name:     "SharedTextPrefixOnly_NotReturned",
			x:        []string{"configuration"},
			y:        []string{"config"},
			expected: nil,
		},
		{
			name:     "DisjointPaths_NotReturned",
			x:        []string{"labels", "config.mode"},
			y:        []string{"owner"},
			expected: nil,
		},
		{
			name:     "RootDiff_EveryFieldReturned",
			x:        []string{""},
			y:        []string{"title", "layout"},
			expected: []string{"title", "layout"},
		},
		{
			name:     "NoDiffPaths_NothingReturned",
			x:        nil,
			y:        []string{"region"},
			expected: nil,
		},
		{
			name:     "MultipleOverlaps_OrderOfYKept",
			x:        []string{"b.x", "a"},
			y:        []string{"a", "b", "c"},
			expected: []string{"a", "b"},
		},
		{
			name:     "MultipleOverlaps_NestedFieldsInX",
			x:        []string{"panels.grid.layout", "types.other.b"},
			y:        []string{"panels.grid", "types.other.c"},
			expected: []string{"panels.grid"},
		},
		{
			name:     "MultipleOverlaps_NestedFieldsInY",
			x:        []string{"widgets.rows"},
			y:        []string{"widgets.rows.height"},
			expected: []string{"widgets.rows.height"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, Intersect(testCase.x, testCase.y))
		})
	}
}

func decode(t *testing.T, raw string) any {
	t.Helper()

	var value any
	require.NoError(t, json.Unmarshal([]byte(raw), &value))

	return value
}
