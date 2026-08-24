package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/clients"
	"github.com/SigNoz/signoz-operator/internal/errors"
	"github.com/SigNoz/signoz-operator/internal/resources"
)

const usersPath = "/api/v2/users"

var (
	_ resources.Adapter = &UserAdapter{}
)

type UserAdapter struct {
	commonTransport
}

func NewUserAdapter() *UserAdapter {
	return &UserAdapter{}
}

func (*UserAdapter) Find(ctx context.Context, c clients.SigNoz, obj resources.Object) ([]*v1alpha1.SigNozResource, error) {
	identity, err := obj.Identity()
	if err != nil {
		return nil, err
	}

	status, result, err := c.Exchange(ctx, http.MethodGet, usersPath, nil)
	if err != nil {
		return nil, err
	}

	if err := errors.NewFromHTTPResponse(status, result); err != nil {
		return nil, err
	}

	data, ok := result["data"]
	if !ok {
		return nil, fmt.Errorf("find: response carries no data")
	}

	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	var users []struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	}
	if err := json.Unmarshal(raw, &users); err != nil {
		return nil, fmt.Errorf("find: could not parse user list: %w", err)
	}

	var matches []*v1alpha1.SigNozResource

	for _, user := range users {
		if strings.EqualFold(user.Email, identity) {
			matches = append(matches, &v1alpha1.SigNozResource{ID: &user.ID})
		}
	}

	return matches, nil
}
