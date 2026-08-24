package jsonbody

import (
	"encoding/json"
	"fmt"

	"github.com/tidwall/gjson"
)

// Extract parses raw and returns the T it encodes; fields of raw that T does
// not declare are dropped.
func Extract[T any](raw string) (T, error) {
	var value T

	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return value, fmt.Errorf("not valid JSON: %w", err)
	}

	return value, nil
}

// ExtractString returns the string at a dot-separated gjson path, or an error
// when the path carries no value.
func ExtractString(raw json.RawMessage, path string) (string, error) {
	value := gjson.GetBytes(raw, path).String()
	if value == "" {
		return "", fmt.Errorf("no value at %s", path)
	}

	return value, nil
}

// ExtractFields returns a document holding just the named top-level fields of
// raw, or nil when raw sets none of them.
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
