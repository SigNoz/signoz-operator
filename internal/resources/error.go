package resources

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// AdapterError is a classified HTTP outcome, produced by Classify. Message may
// quote what the server returned, never what the operator sent, so a
// credential cannot leak into a condition.
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

// NewAdapterError classifies one HTTP outcome, the same way for every kind:
// 400, 401, 403 and 409 are Terminal — a retry will not fix a rejected request
// or credential — and any other non-2xx status (a 5xx, a 429) stays
// Recoverable, so retrying never stops (docs/core-status.md). It returns nil
// for a 2xx.
func NewAdapterError(op AdapterOperation, status int, body map[string]any) *AdapterError {
	if status >= 200 && status < 300 {
		return nil
	}

	outcome := AdapterOutcomeRecoverable

	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict:
		outcome = AdapterOutcomeTerminal
	}

	detail := ""

	if len(body) > 0 {
		if raw, err := json.Marshal(body); err == nil {
			detail = ": " + string(raw)
		}
	}

	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return &AdapterError{
			Operation:      op,
			HTTPStatusCode: status,
			Outcome:        outcome,
			Message:        fmt.Sprintf("%s: SigNoz rejected the credential (HTTP %d)%s", op, status, detail),
		}
	}

	verb := "returned a transient error"
	if outcome == AdapterOutcomeTerminal {
		verb = "rejected the request"
	}

	return &AdapterError{
		Operation:      op,
		HTTPStatusCode: status,
		Outcome:        outcome,
		Message:        fmt.Sprintf("%s: SigNoz %s (HTTP %d)%s", op, verb, status, detail),
	}
}
