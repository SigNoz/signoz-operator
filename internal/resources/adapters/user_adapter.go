package adapters

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"

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

	data := gjson.GetBytes(result, "data")
	if !data.Exists() {
		return nil, errors.New(errors.ReasonInternal, "find: response carries no data")
	}

	var users []struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	}
	if err := json.Unmarshal([]byte(data.Raw), &users); err != nil {
		return nil, errors.Wrap(err, errors.ReasonInternal, "find: could not parse user list")
	}

	var matches []*v1alpha1.SigNozResource

	for _, user := range users {
		if strings.EqualFold(user.Email, identity) {
			matches = append(matches, &v1alpha1.SigNozResource{ID: &user.ID})
		}
	}

	return matches, nil
}
