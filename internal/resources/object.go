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
}
