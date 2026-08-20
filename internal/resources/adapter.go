package resources

import (
	"context"

	"github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/clients"
)

// Adapter implements the transport surface per SigNoz object.
type Adapter interface {
	// Create sends the object's body to SigNoz and returns the metadata of the
	// object it created.
	Create(ctx context.Context, c clients.SigNoz, obj Object) (*v1alpha1.SigNozResource, error)

	// Find resolves the object's identity to the metadata of matching objects:
	// zero, one, or more than one.
	Find(ctx context.Context, c clients.SigNoz, obj Object) ([]*v1alpha1.SigNozResource, error)

	// Observe fetches the object identified by resourceMetadata and reports
	// whether it still exists and whether it is up to date with desired state.
	Observe(ctx context.Context, c clients.SigNoz, obj Object, resourceMetadata *v1alpha1.SigNozResource) (found bool, upToDate bool, err error)

	// Update applies desired state to the object identified by resourceMetadata.
	Update(ctx context.Context, c clients.SigNoz, resourceMetadata *v1alpha1.SigNozResource, obj Object) error

	// Delete removes the object identified by resourceMetadata. An object
	// already gone is not an error.
	Delete(ctx context.Context, c clients.SigNoz, resourceMetadata *v1alpha1.SigNozResource) error
}
