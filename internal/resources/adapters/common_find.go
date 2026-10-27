package adapters

import (
	"context"
	"fmt"
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

// findPageLimit is the page size findByPage asks a paginated list for.
const findPageLimit = 100

// findByPage walks the paginated list at path and returns the metadata of every
// entry under data.<listKey> whose name holds the object's identity. It serves
// the kinds whose list endpoint answers with {data: {<listKey>: [...], total}}
// and offers no server-side filter on name, so every page is walked.
func findByPage(ctx context.Context, c clients.SigNoz, obj resources.Object, path, listKey string) ([]*v1alpha1.SigNozResource, error) {
	identity, err := obj.Identity()
	if err != nil {
		return nil, err
	}

	var matches []*v1alpha1.SigNozResource

	for offset := 0; ; offset += findPageLimit {
		status, result, err := c.Exchange(ctx, http.MethodGet, fmt.Sprintf("%s?limit=%d&offset=%d", path, findPageLimit, offset), nil)
		if err != nil {
			return nil, err
		}

		if err := errors.NewFromHTTPResponse(status, result); err != nil {
			return nil, err
		}

		data := gjson.GetBytes(result, "data")
		if !data.Exists() {
			return nil, errors.New(errors.ReasonInternal, "find: response carries no data")
		}

		list := data.Get(listKey)
		if list.Exists() && !list.IsArray() {
			return nil, errors.New(errors.ReasonInternal, "find: could not parse "+listKey+" list")
		}

		entries := list.Array()
		for _, entry := range entries {
			if entry.Get("name").String() != identity {
				continue
			}

			id := entry.Get("id").String()
			if id == "" {
				return nil, errors.New(errors.ReasonInternal, "find: list entry carries no id")
			}

			matches = append(matches, &v1alpha1.SigNozResource{ID: &id})
		}

		// The empty-page guard terminates the walk even if total overcounts.
		if len(entries) == 0 || offset+len(entries) >= int(data.Get("total").Int()) {
			return matches, nil
		}
	}
}
