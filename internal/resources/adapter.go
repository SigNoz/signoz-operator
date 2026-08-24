package resources

import (
	"context"

	"github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/clients"
)

// Finder is the handwritten half of an adapter: every kind resolves its
// identity to matching SigNoz objects its own way.
type Finder interface {
	// Find resolves the object's identity to the metadata of matching objects:
	// zero, one, or more than one.
	Find(ctx context.Context, c clients.SigNoz, obj Object) ([]*v1alpha1.SigNozResource, error)
}

// Transport is the generated half of an adapter: verbatim endpoint calls with
// no per-kind judgment, stamped out by skaff from skaff.yml and the SigNoz
// OpenAPI spec.
type Transport interface {
	// Create sends the object's body to SigNoz and returns the metadata of the
	// object it created.
	Create(ctx context.Context, c clients.SigNoz, obj Object) (*v1alpha1.SigNozResource, error)

	// Read fetches the object identified by resourceMetadata and returns the
	// raw response; the object owns interpreting it. An object that is gone is
	// (false, nil, nil), not an error.
	Read(ctx context.Context, c clients.SigNoz, resourceMetadata *v1alpha1.SigNozResource) (found bool, response map[string]any, err error)

	// Update applies the object's update payload to the object identified by
	// resourceMetadata. A desired state that manages no updatable field is a
	// no-op success, not an empty write.
	Update(ctx context.Context, c clients.SigNoz, resourceMetadata *v1alpha1.SigNozResource, obj Object) error

	// Delete removes the object identified by resourceMetadata. An object
	// already gone is not an error.
	Delete(ctx context.Context, c clients.SigNoz, resourceMetadata *v1alpha1.SigNozResource) error
}

// Adapter is the transport surface the engine consumes, one implementation per
// SigNoz object: the generated endpoint calls plus the handwritten lookup.
type Adapter interface {
	Transport
	Finder
}
