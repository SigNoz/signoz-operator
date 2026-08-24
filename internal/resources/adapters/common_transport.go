package adapters

import (
	"context"

	"github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/clients"
	"github.com/SigNoz/signoz-operator/internal/errors"
	"github.com/SigNoz/signoz-operator/internal/resources"
)

var (
	_ resources.Transport = (*commonTransport)(nil)
)

type commonTransport struct{}

func (*commonTransport) Create(ctx context.Context, c clients.SigNoz, obj resources.Object) (*v1alpha1.SigNozResource, error) {
	body, err := obj.Body()
	if err != nil {
		return nil, err
	}

	method, path := obj.CreateMethodAndPath()

	status, result, err := c.Exchange(ctx, method, path, body)
	if err != nil {
		return nil, err
	}

	if err := errors.NewFromHTTPResponse(status, result); err != nil {
		return nil, err
	}

	resource, err := obj.ToSigNozResource(result)
	if err != nil {
		return nil, err
	}

	return resource, nil
}

func (*commonTransport) Read(ctx context.Context, c clients.SigNoz, obj resources.Object, resourceMetadata *v1alpha1.SigNozResource) (map[string]any, error) {
	method, path := obj.ReadMethodAndPath(resourceMetadata)

	status, result, err := c.Exchange(ctx, method, path, nil)
	if err != nil {
		return nil, err
	}

	if err := errors.NewFromHTTPResponse(status, result); err != nil {
		return nil, err
	}

	return result, nil
}

func (*commonTransport) Update(ctx context.Context, c clients.SigNoz, obj resources.Object, resourceMetadata *v1alpha1.SigNozResource) error {
	method, path := obj.UpdateMethodAndPath(resourceMetadata)

	payload, err := obj.ToUpdate()
	if err != nil {
		return err
	}

	if payload == nil {
		return nil
	}

	status, result, err := c.Exchange(ctx, method, path, payload)
	if err != nil {
		return err
	}

	if err := errors.NewFromHTTPResponse(status, result); err != nil {
		return err
	}

	return nil
}

func (*commonTransport) Delete(ctx context.Context, c clients.SigNoz, obj resources.Object, resourceMetadata *v1alpha1.SigNozResource) error {
	method, path := obj.DeleteMethodAndPath(resourceMetadata)

	status, result, err := c.Exchange(ctx, method, path, nil)
	if err != nil {
		return err
	}

	if err := errors.NewFromHTTPResponse(status, result); err != nil {
		return err
	}

	return nil
}
