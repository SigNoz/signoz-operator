package jsonbody

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Hash returns a sha256 of the body parsed and re-marshalled, so that
// reindenting a blob, or moving identical content between representations, is
// not a change. Callers share one hashing policy; see docs/resources.md.
func Hash(body json.RawMessage) (string, error) {
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return "", fmt.Errorf("not valid JSON: %w", err)
	}

	canonical, err := json.Marshal(value)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(canonical)

	return hex.EncodeToString(sum[:]), nil
}
