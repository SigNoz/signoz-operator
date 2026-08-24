package resources

import (
	"encoding/json"

	"github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CompareResult struct {
	UpdatableFields []string
	ImmutableFields []string
}

// Object is the mirrored SigNoz object as a Kubernetes custom resource.
type Object interface {
	K8sObject() client.Object

	// Identity returns the key that finds this object in SigNoz.
	Identity() (string, error)

	GetCoreSpec() *v1alpha1.CoreSpec

	GetCoreStatus() *v1alpha1.CoreStatus

	// Body returns the bytes to send to SigNoz.
	Body() (json.RawMessage, error)

	// Hash returns a canonical hash of the rendered body: cosmetic reformatting,
	// and moving identical content between the template forms, must not change it.
	Hash() (string, error)

	// ToSigNozResource extracts the object's identity from a create response.
	ToSigNozResource(response json.RawMessage) (*v1alpha1.SigNozResource, error)

	// ToUpdate returns the update payload from the spec.
	ToUpdate() (json.RawMessage, error)

	// UpdatableFields returns the fields that can be updated.
	UpdatableFields() []string

	// ImmutableFields returns the fields settable only at create.
	ImmutableFields() []string

	// Compare diffs a read response against desired state.
	Compare(response json.RawMessage) (CompareResult, error)

	// CreateMethodAndPath returns the create HTTP method and path.
	CreateMethodAndPath() (string, string)

	// UpdateMethodAndPath returns the update HTTP method and path.
	UpdateMethodAndPath(*v1alpha1.SigNozResource) (string, string)

	// ReadMethodAndPath returns the read HTTP method and path.
	ReadMethodAndPath(*v1alpha1.SigNozResource) (string, string)

	// DeleteMethodAndPath returns the delete HTTP method and path.
	DeleteMethodAndPath(*v1alpha1.SigNozResource) (string, string)
}
