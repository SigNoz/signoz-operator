package adapters

import (
	"context"

	"github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/clients"
	"github.com/SigNoz/signoz-operator/internal/resources"
)

const downtimeSchedulesPath = "/api/v1/downtime_schedules"

var (
	_ resources.Adapter = &PlannedMaintenanceAdapter{}
)

type PlannedMaintenanceAdapter struct {
	commonTransport
}

func NewPlannedMaintenanceAdapter() *PlannedMaintenanceAdapter {
	return &PlannedMaintenanceAdapter{}
}

func (*PlannedMaintenanceAdapter) Find(ctx context.Context, c clients.SigNoz, obj resources.Object) ([]*v1alpha1.SigNozResource, error) {
	return findByField(ctx, c, obj, downtimeSchedulesPath, "name")
}
