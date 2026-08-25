package adapters

import (
	"context"

	"github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/clients"
	"github.com/SigNoz/signoz-operator/internal/resources"
)

const rolesPath = "/api/v1/roles"

var (
	_ resources.Adapter = &RoleAdapter{}
)

type RoleAdapter struct {
	commonTransport
}

func NewRoleAdapter() *RoleAdapter {
	return &RoleAdapter{}
}

func (*RoleAdapter) Find(ctx context.Context, c clients.SigNoz, obj resources.Object) ([]*v1alpha1.SigNozResource, error) {
	return findByField(ctx, c, obj, rolesPath, "name")
}
