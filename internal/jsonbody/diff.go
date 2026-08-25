package jsonbody

import (
	"strconv"
	"strings"

	"github.com/google/go-cmp/cmp"
)

func Equal(x, y any) bool {
	return cmp.Equal(x, y)
}

func Diff(x, y any) []string {
	var reporter pathReporter

	cmp.Equal(x, y, cmp.Reporter(&reporter))

	return reporter.paths
}

// Intersect returns the paths in y that overlap a path in x: equal to it, or
// related as ancestor and descendant. The root path "" overlaps every path.
func Intersect(x, y []string) []string {
	var intersection []string

	for _, candidate := range y {
		for _, path := range x {
			if overlaps(candidate, path) {
				intersection = append(intersection, candidate)
				break
			}
		}
	}

	return intersection
}

func overlaps(x, y string) bool {
	if x == y || x == "" || y == "" {
		return true
	}

	return strings.HasPrefix(x, y+".") || strings.HasPrefix(y, x+".")
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
