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
	identity, err := obj.Identity()
	if err != nil {
		return nil, err
	}

	status, result, err := c.Exchange(ctx, http.MethodGet, authDomainsPath, nil)
	if err != nil {
		return nil, err
	}

	if err := errors.NewFromHTTPResponse(status, result); err != nil {
		return nil, err
	}

	data := gjson.GetBytes(result, "data")
	if !data.Exists() {
		return nil, fmt.Errorf("find: response carries no data")
	}

	var domains []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(data.Raw), &domains); err != nil {
		return nil, fmt.Errorf("find: could not parse auth domain list: %w", err)
	}

	var matches []*v1alpha1.SigNozResource

	for _, domain := range domains {
		if domain.Name == identity {
			matches = append(matches, &v1alpha1.SigNozResource{ID: &domain.ID})
		}
	}

	return matches, nil
}
