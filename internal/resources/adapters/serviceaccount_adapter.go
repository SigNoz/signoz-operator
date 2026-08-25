package adapters

import (
	"context"

	"github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/clients"
	"github.com/SigNoz/signoz-operator/internal/resources"
)

const serviceAccountsPath = "/api/v1/service_accounts"

var (
	_ resources.Adapter = &ServiceAccountAdapter{}
)

type ServiceAccountAdapter struct {
	commonTransport
}

func NewServiceAccountAdapter() *ServiceAccountAdapter {
	return &ServiceAccountAdapter{}
}

func (*ServiceAccountAdapter) Find(ctx context.Context, c clients.SigNoz, obj resources.Object) ([]*v1alpha1.SigNozResource, error) {
	return findByField(ctx, c, obj, serviceAccountsPath, "name")
}
