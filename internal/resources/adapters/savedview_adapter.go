package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/clients"
	"github.com/SigNoz/signoz-operator/internal/errors"
	"github.com/SigNoz/signoz-operator/internal/resources"
)

const savedViewsPath = "/api/v2/saved_views"

var (
	_ resources.Adapter = &SavedViewAdapter{}
)

type SavedViewAdapter struct {
	commonTransport
}

func NewSavedViewAdapter() *SavedViewAdapter {
	return &SavedViewAdapter{}
}

func (*SavedViewAdapter) Find(ctx context.Context, c clients.SigNoz, obj resources.Object) ([]*v1alpha1.SigNozResource, error) {
	identity, err := obj.Identity()
	if err != nil {
		return nil, err
	}

	// The name parameter narrows the list server-side; its match semantics are
	// the server's, so names are still compared exactly here.
	status, result, err := c.Exchange(ctx, http.MethodGet, savedViewsPath+"?name="+url.QueryEscape(identity), nil)
	if err != nil {
		return nil, err
	}

	if err := errors.NewFromHTTPResponse(status, result); err != nil {
		return nil, err
	}

	data, ok := result["data"]
	if !ok {
		return nil, fmt.Errorf("find: response carries no data")
	}

	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	var views []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &views); err != nil {
		return nil, fmt.Errorf("find: could not parse saved view list: %w", err)
	}

	var matches []*v1alpha1.SigNozResource

	for _, view := range views {
		if view.Name == identity {
			matches = append(matches, &v1alpha1.SigNozResource{ID: &view.ID})
		}
	}

	return matches, nil
}
