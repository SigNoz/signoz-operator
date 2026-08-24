package resources

import (
	"encoding/json"

	"github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Object is the mirrored SigNoz object as a Kubernetes custom resource.
type Object interface {
	K8sObject() client.Object

	// Identity returns the key that finds this object in SigNoz, derived purely
	// from desired state so that it is the same on every form of the body.
	Identity() (string, error)

	GetCoreSpec() *v1alpha1.CoreSpec

	GetCoreStatus() *v1alpha1.CoreStatus

	// Body returns the bytes to send to SigNoz.
	Body() (json.RawMessage, error)

	// Hash returns a canonical hash of the rendered body: cosmetic reformatting,
	// and moving identical content between the template forms, must not change it.
	Hash() (string, error)

	// ToSigNozResource extracts the object's identity from a create response, by
	// the kind's response paths.
	ToSigNozResource(response map[string]any) (*v1alpha1.SigNozResource, error)

	// ToUpdate returns the update payload: desired state projected onto the
	// fields the update endpoint accepts, nil when it manages none of them.
	ToUpdate() (json.RawMessage, error)

	// UpdatableFields returns the fields the update endpoint accepts, from the
	// OpenAPI update request schema.
	UpdatableFields() []string

	// ImmutableFields returns the fields settable only at create: the create
	// request schema minus the updatable fields.
	ImmutableFields() []string

	// Compare diffs a read response against desired state and classifies the
	// drift. A field absent from the desired body is unmanaged, never "blank
	// it"; an immutable field the response does not carry is not compared.
	Compare(response map[string]any) (Drift, error)
}

// Drift is Compare's verdict: the fields whose remote value differs from
// desired, split by consequence — updatable drift is fixed by an update,
// immutable drift never can be, the fields are settable only at create.
type Drift struct {
	UpdatableFields []string
	ImmutableFields []string
}
