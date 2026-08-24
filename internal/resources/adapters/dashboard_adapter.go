package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/clients"
	"github.com/SigNoz/signoz-operator/internal/errors"
	"github.com/SigNoz/signoz-operator/internal/resources"
)

const (
	dashboardsPath      = "/api/v2/dashboards"
	dashboardsPageLimit = 100
)

var (
	_ resources.Adapter = &DashboardAdapter{}
)

type DashboardAdapter struct {
	commonTransport
}

func NewDashboardAdapter() *DashboardAdapter {
	return &DashboardAdapter{}
}

func (*DashboardAdapter) Find(ctx context.Context, c clients.SigNoz, obj resources.Object) ([]*v1alpha1.SigNozResource, error) {
	identity, err := obj.Identity()
	if err != nil {
		return nil, err
	}

	var matches []*v1alpha1.SigNozResource

	for offset := 0; ; offset += dashboardsPageLimit {
		path := fmt.Sprintf("%s?limit=%d&offset=%d", dashboardsPath, dashboardsPageLimit, offset)

		status, result, err := c.Exchange(ctx, http.MethodGet, path, nil)
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

		var page struct {
			Dashboards []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"dashboards"`
			Total int `json:"total"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, fmt.Errorf("find: could not parse dashboard list: %w", err)
		}

		for _, dashboard := range page.Dashboards {
			if dashboard.Name == identity {
				matches = append(matches, &v1alpha1.SigNozResource{ID: &dashboard.ID})
			}
		}

		// The empty-page guard terminates the walk even if total overcounts.
		if len(page.Dashboards) == 0 || offset+len(page.Dashboards) >= page.Total {
			return matches, nil
		}
	}
}
