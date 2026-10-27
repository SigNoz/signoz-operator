package adapters

import (
	"context"

	"github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/clients"
	"github.com/SigNoz/signoz-operator/internal/resources"
)

const dashboardsPath = "/api/v2/dashboards"

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
	return findByPage(ctx, c, obj, dashboardsPath, "dashboards")
}
