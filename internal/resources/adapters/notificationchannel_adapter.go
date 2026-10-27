package adapters

import (
	"context"

	"github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/clients"
	"github.com/SigNoz/signoz-operator/internal/resources"
)

var (
	_ resources.Adapter = &NotificationChannelAdapter{}
)

type NotificationChannelAdapter struct {
	commonTransport
}

func NewNotificationChannelAdapter() *NotificationChannelAdapter {
	return &NotificationChannelAdapter{}
}

func (*NotificationChannelAdapter) Find(ctx context.Context, c clients.SigNoz, obj resources.Object) ([]*v1alpha1.SigNozResource, error) {
	return nil, nil
}
