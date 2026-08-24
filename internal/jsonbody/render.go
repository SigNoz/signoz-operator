package jsonbody

import (
	"encoding/json"
	"errors"
)

// Render returns the bytes a two-form template encodes: the typed form
// marshalled, jsonSpec verbatim.
func Render[T any](spec *T, jsonSpec *string) (json.RawMessage, error) {
	switch {
	case spec != nil:
		return json.Marshal(spec)
	case jsonSpec != nil:
		return json.RawMessage(*jsonSpec), nil
	default:
		return nil, errors.New("objectTemplate: exactly one of spec or jsonSpec must be set")
	}
}
