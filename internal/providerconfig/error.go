package providerconfig

// Reasons reported on the Ready condition. Neither a reason nor the message built
// alongside it carries a resolved value, so both are safe to copy into status.
var (
	ReasonResolved = Reason{s: "Resolved"}

	// ReasonSpecInvalid is a spec the schema should have rejected — an auth block
	// with no method set, a valueFrom with no source.
	ReasonSpecInvalid = Reason{s: "SpecInvalid"}

	ReasonEndpointInvalid   = Reason{s: "EndpointInvalid"}
	ReasonSecretNotFound    = Reason{s: "SecretNotFound"}
	ReasonConfigMapNotFound = Reason{s: "ConfigMapNotFound"}
	ReasonKeyNotFound       = Reason{s: "KeyNotFound"}
	ReasonValueEmpty        = Reason{s: "ValueEmpty"}
	ReasonCABundleInvalid   = Reason{s: "CABundleInvalid"}

	// ReasonReferenceReadFailed is a read that failed for a reason other than the
	// object not existing. The operator retries this one itself.
	ReasonReferenceReadFailed = Reason{s: "ReferenceReadFailed"}
)

// Reason is one of the fixed reasons above; the unexported field keeps the
// vocabulary closed.
type Reason struct{ s string }

func (r Reason) String() string {
	return r.s
}

// The resolve outcomes — how a resolution failure gets back on its feet.
// WaitForWatch means the fix is an edit to the spec or to a referenced object,
// and the watch on it brings the reconcile back; Retry means the operator must
// come back on its own. The reconciler retries anything that is not explicitly
// WaitForWatch — the zero value included — because retrying a permanent failure
// is bounded by backoff, while waiting on a transient one can strand the object
// until resync.
var (
	ResolveOutcomeWaitForWatch = ResolveOutcome{s: "wait-for-watch"}
	ResolveOutcomeRetry        = ResolveOutcome{s: "retry"}
)

// ResolveOutcome is one of the outcomes above; the unexported field keeps the
// vocabulary closed.
type ResolveOutcome struct{ s string }

func (o ResolveOutcome) String() string {
	return o.s
}

// Error is a resolution failure carrying the reason to report on Ready and the
// outcome that decides the requeue. Its message names the field, object and key
// at fault and never a resolved value, because a condition message is copied
// wherever conditions are.
type Error struct {
	Reason  Reason
	Message string
	Outcome ResolveOutcome

	cause error
}

func (e *Error) Error() string { return e.Message }

func (e *Error) Unwrap() error { return e.cause }

// at prefixes the message with the spec field the failure was reached through, so
// one Secret read for two fields reports the field that needed it.
func (e *Error) at(path string) *Error {
	return &Error{
		Reason:  e.Reason,
		Message: path + ": " + e.Message,
		Outcome: e.Outcome,
		cause:   e.cause,
	}
}
