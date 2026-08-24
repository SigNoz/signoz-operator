package objects

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
)

// Every mirrored kind carries an objectTemplate with the same two-form shape:
// typed fields the API server validates (spec), or a raw request body sent
// verbatim (jsonSpec), exactly one of which is set. controller-gen cannot
// generate a CRD schema from a generic type, so each kind declares its
// concrete template and shares the behaviour below.

// renderTemplate returns the bytes to send to SigNoz: the typed form
// marshalled, jsonSpec verbatim.
func renderTemplate[T any](spec *T, jsonSpec *string) (json.RawMessage, error) {
	switch {
	case spec != nil:
		return json.Marshal(spec)
	case jsonSpec != nil:
		return json.RawMessage(*jsonSpec), nil
	default:
		return nil, errors.New("objectTemplate: exactly one of spec or jsonSpec must be set")
	}
}

// canonicalHash hashes a rendered body parsed and re-marshalled, so that
// reindenting a jsonSpec blob, or moving identical content between forms, is
// not drift. Every kind's Hash must use it: the hashing policy is uniform
// across kinds. See docs/resources.md.
func canonicalHash(body json.RawMessage) (string, error) {
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

// pathByID joins a kind's collection path and the id recorded in resource
// metadata; the engine only asks for an object's path once a create or lookup
// has filled the id in.
func pathByID(collectionPath string, resourceMetadata *v1alpha1.SigNozResource) string {
	id, _ := v1alpha1.GetIDFromSigNozResource(resourceMetadata)

	return collectionPath + "/" + id
}
