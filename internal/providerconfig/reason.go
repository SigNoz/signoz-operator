package providerconfig

var (
	ReasonResolved            = Reason{s: "Resolved"}
	ReasonSpecInvalid         = Reason{s: "SpecInvalid"}
	ReasonEndpointInvalid     = Reason{s: "EndpointInvalid"}
	ReasonSecretNotFound      = Reason{s: "SecretNotFound"}
	ReasonConfigMapNotFound   = Reason{s: "ConfigMapNotFound"}
	ReasonKeyNotFound         = Reason{s: "KeyNotFound"}
	ReasonValueEmpty          = Reason{s: "ValueEmpty"}
	ReasonCABundleInvalid     = Reason{s: "CABundleInvalid"}
	ReasonReferenceReadFailed = Reason{s: "ReferenceReadFailed"}
)

// Reasons reported on the Ready condition.
type Reason struct{ s string }

func (r Reason) String() string {
	return r.s
}
