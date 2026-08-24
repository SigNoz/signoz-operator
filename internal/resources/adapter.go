package resources

import (
	"context"
	"encoding/json"

	"github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/clients"
)

type Finder interface {
	// Find resolves the object's identity to the metadata of matching objects:
	// zero, one, or more than one.
	Find(ctx context.Context, c clients.SigNoz, obj Object) ([]*v1alpha1.SigNozResource, error)
}

type Transport interface {
	// Create sends the object's body to SigNoz and returns the metadata of the object it created.
	Create(ctx context.Context, c clients.SigNoz, obj Object) (*v1alpha1.SigNozResource, error)

	// Read fetches the object identified by resourceMetadata and returns the raw response.
	Read(ctx context.Context, c clients.SigNoz, obj Object, resourceMetadata *v1alpha1.SigNozResource) (json.RawMessage, error)

	// Update applies the object's update payload to the object identified by resourceMetadata
	Update(ctx context.Context, c clients.SigNoz, obj Object, resourceMetadata *v1alpha1.SigNozResource) error

	// Delete removes the object identified by resourceMetadata. An object already gone is not an error.
	Delete(ctx context.Context, c clients.SigNoz, obj Object, resourceMetadata *v1alpha1.SigNozResource) error
}

type Adapter interface {
	Transport
	Finder
}
