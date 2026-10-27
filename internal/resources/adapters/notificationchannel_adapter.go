package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/tidwall/gjson"

	"github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/clients"
	"github.com/SigNoz/signoz-operator/internal/errors"
	"github.com/SigNoz/signoz-operator/internal/resources"
)

const (
	notificationChannelsPath      = "/api/v2/notification_channels"
	notificationChannelsPageLimit = 100
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
	identity, err := obj.Identity()
	if err != nil {
		return nil, err
	}

	var matches []*v1alpha1.SigNozResource

	// The list's query parameter matches displayName, not name, so every page is walked.
	for offset := 0; ; offset += notificationChannelsPageLimit {
		path := fmt.Sprintf("%s?limit=%d&offset=%d", notificationChannelsPath, notificationChannelsPageLimit, offset)

		status, result, err := c.Exchange(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}

		if err := errors.NewFromHTTPResponse(status, result); err != nil {
			return nil, err
		}

		data := gjson.GetBytes(result, "data")
		if !data.Exists() {
			return nil, errors.New(errors.ReasonInternal, "find: response carries no data")
		}

		var page struct {
			Channels []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"channels"`
			Total int `json:"total"`
		}
		if err := json.Unmarshal([]byte(data.Raw), &page); err != nil {
			return nil, errors.Wrap(err, errors.ReasonInternal, "find: could not parse notification channel list")
		}

		for _, channel := range page.Channels {
			if channel.Name == identity {
				matches = append(matches, &v1alpha1.SigNozResource{ID: &channel.ID})
			}
		}

		if len(page.Channels) == 0 || offset+len(page.Channels) >= page.Total {
			return matches, nil
		}
	}
}
