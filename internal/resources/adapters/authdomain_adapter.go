package adapters

import (
	"context"

	"github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/clients"
	"github.com/SigNoz/signoz-operator/internal/resources"
)

const authDomainsPath = "/api/v2/auth_domains"

var (
	_ resources.Adapter = &AuthDomainAdapter{}
)

type AuthDomainAdapter struct {
	commonTransport
}

func NewAuthDomainAdapter() *AuthDomainAdapter {
	return &AuthDomainAdapter{}
}

func (*AuthDomainAdapter) Find(ctx context.Context, c clients.SigNoz, obj resources.Object) ([]*v1alpha1.SigNozResource, error) {
	return findByField(ctx, c, obj, authDomainsPath, "name")
}
