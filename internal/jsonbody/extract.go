package jsonbody

import (
	"encoding/json"
	"fmt"

	"github.com/tidwall/gjson"
)

// ExtractFromSpecs renders whichever spec form is set and returns the string at path.
func ExtractFromSpecs[T any](spec *T, jsonSpec *string, path string) (string, error) {
	body, err := Render(spec, jsonSpec)
	if err != nil {
		return "", err
	}

	return ExtractString(body, path)
}

func ExtractString(raw json.RawMessage, path string) (string, error) {
	value := gjson.GetBytes(raw, path).String()
	if value == "" {
		return "", fmt.Errorf("no value at %s", path)
	}

	return value, nil
}

func ExtractFields(raw json.RawMessage, fields []string) (json.RawMessage, error) {
	payload := map[string]any{}

	for _, field := range fields {
		if value := gjson.GetBytes(raw, field); value.Exists() {
			payload[field] = value.Value()
		}
	}

	if len(payload) == 0 {
		return nil, nil
	}

	return json.Marshal(payload)
}
