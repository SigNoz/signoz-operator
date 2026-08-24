package errors

var (
	ReasonUnknown         = Reason{s: "Unknown"}
	ReasonInvalidInput    = Reason{s: "InvalidInput"}
	ReasonUnauthorized    = Reason{s: "Unauthorized"}
	ReasonForbidden       = Reason{s: "Forbidden"}
	ReasonNotFound        = Reason{s: "NotFound"}
	ReasonAlreadyExists   = Reason{s: "AlreadyExists"}
	ReasonTooManyRequests = Reason{s: "TooManyRequests"}
	ReasonInternal        = Reason{s: "Internal"}
	ReasonUnreachable     = Reason{s: "Unreachable"}
)

// Reason classifies an error by cause, so callers branch on it instead of on
// per-package error structs.
type Reason struct{ s string }

func (r Reason) String() string {
	return r.s
}

// Retryable reports whether a retry may fix the failure. An unknown cause is
// retryable: the outcome is unsettled, and retrying never strands the object.
func (r Reason) Retryable() bool {
	switch r {
	case ReasonInvalidInput, ReasonUnauthorized, ReasonForbidden, ReasonAlreadyExists:
		return false
	}

	return true
}
