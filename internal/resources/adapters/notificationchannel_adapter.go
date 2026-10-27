package adapters

import (
	"context"

	"github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/clients"
	"github.com/SigNoz/signoz-operator/internal/resources"
)

const notificationChannelsPath = "/api/v2/notification_channels"

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
	// The list's query parameter matches displayName, not name, so every page is walked.
	return findByPage(ctx, c, obj, notificationChannelsPath, "channels")
}
