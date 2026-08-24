package jsonbody

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/google/go-cmp/cmp"
	"github.com/tidwall/gjson"
)

// Equal reports whether a and b are exactly equal. Both values must come from
// encoding/json decoding (or gjson's Value), so numbers are float64 on both
// sides.
func Equal(a, b any) bool {
	return cmp.Equal(a, b)
}

// Diff returns the dot-separated paths at which a and b diverge, empty when
// they are exactly equal. A divergence at the root has path "".
func Diff(a, b any) []string {
	var reporter pathReporter

	cmp.Equal(a, b, cmp.Reporter(&reporter))

	return reporter.paths
}

// FieldDiff lists the top-level fields of a desired body whose values diverge
// from the remote document.
type FieldDiff struct {
	Updatable []string
	Immutable []string
}

// DiffWithFields diffs desired against remote, field by field: fields desired
// does not set are skipped, fields it sets must match exactly. Immutable
// fields remote omits are skipped rather than flagged, so an incomplete read
// cannot present as an immutable change.
func DiffWithFields(desired, remote json.RawMessage, updatable, immutable []string) FieldDiff {
	var diff FieldDiff

	for _, field := range updatable {
		value := gjson.GetBytes(desired, field)
		if !value.Exists() {
			continue
		}

		if !Equal(value.Value(), gjson.GetBytes(remote, field).Value()) {
			diff.Updatable = append(diff.Updatable, field)
		}
	}

	for _, field := range immutable {
		desiredValue := gjson.GetBytes(desired, field)
		if !desiredValue.Exists() {
			continue
		}

		remoteValue := gjson.GetBytes(remote, field)
		if !remoteValue.Exists() {
			continue
		}

		if !Equal(desiredValue.Value(), remoteValue.Value()) {
			diff.Immutable = append(diff.Immutable, field)
		}
	}

	return diff
}

type pathReporter struct {
	steps cmp.Path
	paths []string
}

func (r *pathReporter) PushStep(step cmp.PathStep) {
	r.steps = append(r.steps, step)
}

func (r *pathReporter) Report(result cmp.Result) {
	if !result.Equal() {
		r.paths = append(r.paths, formatPath(r.steps))
	}
}

func (r *pathReporter) PopStep() {
	r.steps = r.steps[:len(r.steps)-1]
}

func formatPath(steps cmp.Path) string {
	segments := make([]string, 0, len(steps))

	for _, step := range steps {
		switch step := step.(type) {
		case cmp.MapIndex:
			segments = append(segments, step.Key().String())
		case cmp.SliceIndex:
			segments = append(segments, strconv.Itoa(sliceKey(step)))
		}
	}

	return strings.Join(segments, ".")
}

// sliceKey resolves the index when the sides are misaligned: an element
// present on only one side reports -1 for the side that lacks it.
func sliceKey(step cmp.SliceIndex) int {
	if key := step.Key(); key >= 0 {
		return key
	}

	xkey, ykey := step.SplitKeys()
	if xkey >= 0 {
		return xkey
	}

	return ykey
}
