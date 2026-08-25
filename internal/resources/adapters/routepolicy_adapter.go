package adapters

import (
	"context"

	"github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/clients"
	"github.com/SigNoz/signoz-operator/internal/resources"
)

const routePoliciesPath = "/api/v1/route_policies"

var (
	_ resources.Adapter = &RoutePolicyAdapter{}
)

type RoutePolicyAdapter struct {
	commonTransport
}

func NewRoutePolicyAdapter() *RoutePolicyAdapter {
	return &RoutePolicyAdapter{}
}

func (*RoutePolicyAdapter) Find(ctx context.Context, c clients.SigNoz, obj resources.Object) ([]*v1alpha1.SigNozResource, error) {
	return findByField(ctx, c, obj, routePoliciesPath, "name")
}
