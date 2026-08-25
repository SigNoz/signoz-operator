package jsonbody

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func Hash(body json.RawMessage) (string, error) {
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return "", err
	}

	canonical, err := json.Marshal(value)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(canonical)

	return hex.EncodeToString(sum[:]), nil
}
