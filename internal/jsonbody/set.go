package jsonbody

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/SigNoz/signoz-operator/internal/errors"
)

// Set returns body with value at path, a dot-separated list of object keys and
// list indexes. Missing objects along the path are created; a list is never
// grown. No error carries the value.
func Set(body json.RawMessage, path string, value string) (json.RawMessage, error) {
	// UseNumber keeps numbers elsewhere in the body exactly as they were written.
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()

	var root any
	if err := decoder.Decode(&root); err != nil {
		return nil, err
	}

	root, err := set(root, strings.Split(path, "."), value, path)
	if err != nil {
		return nil, err
	}

	return json.Marshal(root)
}

func set(node any, segments []string, value string, path string) (any, error) {
	if len(segments) == 0 {
		return value, nil
	}

	segment := segments[0]

	switch container := node.(type) {
	case map[string]any:
		child, err := set(container[segment], segments[1:], value, path)
		if err != nil {
			return nil, err
		}

		container[segment] = child

		return container, nil
	case []any:
		index, err := strconv.Atoi(segment)
		if err != nil || index < 0 || index >= len(container) {
			return nil, errors.Newf(errors.ReasonInvalidInput, "path %q: %q is not an index into a list of %d", path, segment, len(container))
		}

		child, err := set(container[index], segments[1:], value, path)
		if err != nil {
			return nil, err
		}

		container[index] = child

		return container, nil
	case nil:
		return set(map[string]any{}, segments, value, path)
	default:
		return nil, errors.Newf(errors.ReasonInvalidInput, "path %q: %q is set on a value that is not an object or a list", path, segment)
	}
}
