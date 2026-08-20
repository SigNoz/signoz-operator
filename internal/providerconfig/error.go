package providerconfig

// Reasons reported on the Ready condition; none carries a resolved value.
var (
	ReasonResolved = Reason{s: "Resolved"}

	// A spec the schema should have rejected.
	ReasonSpecInvalid = Reason{s: "SpecInvalid"}

	ReasonEndpointInvalid   = Reason{s: "EndpointInvalid"}
	ReasonSecretNotFound    = Reason{s: "SecretNotFound"}
	ReasonConfigMapNotFound = Reason{s: "ConfigMapNotFound"}
	ReasonKeyNotFound       = Reason{s: "KeyNotFound"}
	ReasonValueEmpty        = Reason{s: "ValueEmpty"}
	ReasonCABundleInvalid   = Reason{s: "CABundleInvalid"}

	// A read that failed other than by the object not existing.
	ReasonReferenceReadFailed = Reason{s: "ReferenceReadFailed"}
)

// Reason is one of the fixed reasons above, closed by its unexported field.
type Reason struct{ s string }

func (r Reason) String() string {
	return r.s
}

// How a resolution failure gets back on its feet: WaitForWatch, an edit the
// operator watches brings the reconcile back; Retry, the operator comes back on
// its own. Anything not explicitly WaitForWatch — the zero value included — retries.
var (
	ResolveOutcomeWaitForWatch = ResolveOutcome{s: "wait-for-watch"}
	ResolveOutcomeRetry        = ResolveOutcome{s: "retry"}
)

// ResolveOutcome is one of the outcomes above, closed by its unexported field.
type ResolveOutcome struct{ s string }

func (o ResolveOutcome) String() string {
	return o.s
}

// Error is a resolution failure carrying the Ready reason and the requeue outcome.
// Message names the field, object and key at fault, never a resolved value.
type Error struct {
	Reason  Reason
	Message string
	Outcome ResolveOutcome

	cause error
}

func (e *Error) Error() string { return e.Message }

func (e *Error) Unwrap() error { return e.cause }
