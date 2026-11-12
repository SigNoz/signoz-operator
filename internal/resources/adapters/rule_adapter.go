package adapters

import (
	"context"

	"github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/clients"
	"github.com/SigNoz/signoz-operator/internal/resources"
)

const rulesPath = "/api/v3/rules"

var (
	_ resources.Adapter = &RuleAdapter{}
)

type RuleAdapter struct {
	commonTransport
}

func NewRuleAdapter() *RuleAdapter {
	return &RuleAdapter{}
}

func (*RuleAdapter) Find(ctx context.Context, c clients.SigNoz, obj resources.Object) ([]*v1alpha1.SigNozResource, error) {
	return findByPage(ctx, c, obj, rulesPath, "rules", "alert")
}
