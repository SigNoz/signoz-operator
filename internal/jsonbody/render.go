package jsonbody

import (
	"encoding/json"

	"github.com/SigNoz/signoz-operator/internal/errors"
)

func Render[T any](spec *T, jsonSpec *string) (json.RawMessage, error) {
	if spec != nil {
		out, err := json.Marshal(spec)
		if err != nil {
			return nil, err
		}

		return out, nil
	}

	if jsonSpec != nil {
		var value any
		if err := json.Unmarshal([]byte(*jsonSpec), &value); err != nil {
			return nil, err
		}

		return json.RawMessage(*jsonSpec), nil
	}

	return nil, errors.New(errors.ReasonInvalidInput, "exactly one of spec or jsonSpec must be set")
}
