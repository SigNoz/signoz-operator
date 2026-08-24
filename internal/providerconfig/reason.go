package providerconfig

import (
	"github.com/SigNoz/signoz-operator/internal/errors"
)

var (
	ReasonResolved = Reason{s: "Resolved"}
)

// Reasons reported on the Ready condition.
type Reason struct{ s string }

func (r Reason) String() string {
	return r.s
}

// Failure reasons travel as error codes on the resolver's errors; SetConditions
// reports them on the Ready condition.
var (
	CodeSpecInvalid         = errors.NewCode("SpecInvalid")
	CodeEndpointInvalid     = errors.NewCode("EndpointInvalid")
	CodeSecretNotFound      = errors.NewCode("SecretNotFound")
	CodeConfigMapNotFound   = errors.NewCode("ConfigMapNotFound")
	CodeKeyNotFound         = errors.NewCode("KeyNotFound")
	CodeValueEmpty          = errors.NewCode("ValueEmpty")
	CodeCABundleInvalid     = errors.NewCode("CABundleInvalid")
	CodeReferenceReadFailed = errors.NewCode("ReferenceReadFailed")
)
