package adapters

import (
	"context"
	"net/http"

	"github.com/tidwall/gjson"

	"github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/clients"
	"github.com/SigNoz/signoz-operator/internal/errors"
	"github.com/SigNoz/signoz-operator/internal/resources"
)

// findByField lists path and returns the metadata of every entry whose field
// holds the object's identity. It serves the kinds whose list endpoint answers
// with a flat data array and no server-side filter.
func findByField(ctx context.Context, c clients.SigNoz, obj resources.Object, path, field string) ([]*v1alpha1.SigNozResource, error) {
	identity, err := obj.Identity()
	if err != nil {
		return nil, err
	}

	status, result, err := c.Exchange(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	if err := errors.NewFromHTTPResponse(status, result); err != nil {
		return nil, err
	}

	data := gjson.GetBytes(result, "data")
	if !data.Exists() || !data.IsArray() {
		return nil, errors.New(errors.ReasonInternal, "find: response carries no data list")
	}

	var matches []*v1alpha1.SigNozResource

	for _, entry := range data.Array() {
		if entry.Get(field).String() != identity {
			continue
		}

		id := entry.Get("id").String()
		if id == "" {
			return nil, errors.New(errors.ReasonInternal, "find: list entry carries no id")
		}

		matches = append(matches, &v1alpha1.SigNozResource{ID: &id})
	}

	return matches, nil
}
