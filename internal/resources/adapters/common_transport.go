package adapters

import (
	"context"
	"fmt"
	"net/http"

	"github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/clients"
	"github.com/SigNoz/signoz-operator/internal/resources"
)

var (
	_ resources.Transport = transport{}
)

// transport is the Transport half shared by every kind whose endpoints follow
// the collection / collection-by-id shape; a kind's adapter embeds it with its
// collection path and adds the handwritten Find.
type transport struct {
	collectionPath string
}

func (t transport) byIDPath(id string) string { return t.collectionPath + "/" + id }

// Create sends the rendered body and returns the metadata SigNoz assigned. A
// 2xx whose body cannot be read returns a plain error: the object was created
// but its id is unknown, which the engine recovers from by finding it by
// identity.
func (t transport) Create(ctx context.Context, c clients.SigNoz, obj resources.Object) (*v1alpha1.SigNozResource, error) {
	body, err := obj.Body()
	if err != nil {
		return nil, err
	}

	status, result, err := c.Exchange(ctx, http.MethodPost, t.collectionPath, body)
	if err != nil {
		return nil, err
	}

	if apiErr := resources.NewAdapterError(resources.AdapterOperationCreate, status, result); apiErr != nil {
		return nil, apiErr
	}

	resource, err := obj.ToSigNozResource(result)
	if err != nil {
		return nil, fmt.Errorf("create: %w", err)
	}

	return resource, nil
}

// Read fetches the object by id and returns the raw response; the object owns
// interpreting it. An object that is gone is (false, nil, nil), not an error.
func (t transport) Read(ctx context.Context, c clients.SigNoz, resourceMetadata *v1alpha1.SigNozResource) (bool, map[string]any, error) {
	id, err := v1alpha1.GetIDFromSigNozResource(resourceMetadata)
	if err != nil {
		return false, nil, err
	}

	status, result, err := c.Exchange(ctx, http.MethodGet, t.byIDPath(id), nil)
	if err != nil {
		return false, nil, err
	}

	if status == http.StatusNotFound {
		return false, nil, nil
	}

	if apiErr := resources.NewAdapterError(resources.AdapterOperationRead, status, result); apiErr != nil {
		return false, nil, apiErr
	}

	return true, result, nil
}

// Update sends the object's update payload; a desired state that manages no
// updatable field is a no-op success, not an empty write.
func (t transport) Update(ctx context.Context, c clients.SigNoz, resourceMetadata *v1alpha1.SigNozResource, obj resources.Object) error {
	id, err := v1alpha1.GetIDFromSigNozResource(resourceMetadata)
	if err != nil {
		return err
	}

	payload, err := obj.ToUpdate()
	if err != nil {
		return err
	}

	if payload == nil {
		return nil
	}

	status, result, err := c.Exchange(ctx, http.MethodPut, t.byIDPath(id), payload)
	if err != nil {
		return err
	}

	if apiErr := resources.NewAdapterError(resources.AdapterOperationUpdate, status, result); apiErr != nil {
		return apiErr
	}

	return nil
}

// Delete removes the object by id. An object already gone is not an error.
func (t transport) Delete(ctx context.Context, c clients.SigNoz, resourceMetadata *v1alpha1.SigNozResource) error {
	id, err := v1alpha1.GetIDFromSigNozResource(resourceMetadata)
	if err != nil {
		return err
	}

	status, result, err := c.Exchange(ctx, http.MethodDelete, t.byIDPath(id), nil)
	if err != nil {
		return err
	}

	if status == http.StatusNotFound {
		return nil
	}

	if apiErr := resources.NewAdapterError(resources.AdapterOperationDelete, status, result); apiErr != nil {
		return apiErr
	}

	return nil
}
