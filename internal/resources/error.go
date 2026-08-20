package resources

import "net/http"

// AdapterError is a classified HTTP outcome. A status the adapter is not sure
// about must stay Recoverable (docs/core-status.md). Message may quote what
// the server returned, never what the operator sent, so a credential cannot
// leak into a condition.
type AdapterError struct {
	Operation      AdapterOperation
	HTTPStatusCode int
	Message        string
	Outcome        AdapterOutcome
}

func (e *AdapterError) Error() string { return e.Message }

// AttributableToProvider reports whether the failure is the provider config's:
// the server rejected the credential sent on its behalf.
func (e *AdapterError) AttributableToProvider() bool {
	return e.HTTPStatusCode == http.StatusUnauthorized || e.HTTPStatusCode == http.StatusForbidden
}
