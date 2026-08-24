package errors

import (
	"bytes"
	"encoding/json"
	"net/http"
)

func NewFromHTTPResponse(status int, body json.RawMessage) *Base {
	if status >= 200 && status < 300 {
		return nil
	}

	detail := ""

	if len(body) > 0 {
		compact := &bytes.Buffer{}
		if err := json.Compact(compact, body); err == nil {
			detail = ": " + compact.String()
		}
	}

	switch status {
	case http.StatusBadRequest:
		return Newf(ReasonInvalidInput, "the server rejected the request (HTTP %d)%s", status, detail)
	case http.StatusUnauthorized:
		return Newf(ReasonUnauthorized, "the server rejected the credential (HTTP %d)%s", status, detail)
	case http.StatusForbidden:
		return Newf(ReasonForbidden, "the server rejected the credential (HTTP %d)%s", status, detail)
	case http.StatusNotFound:
		return Newf(ReasonNotFound, "the server could not find the object (HTTP %d)%s", status, detail)
	case http.StatusConflict:
		return Newf(ReasonAlreadyExists, "the server reported a conflict (HTTP %d)%s", status, detail)
	case http.StatusTooManyRequests:
		return Newf(ReasonTooManyRequests, "the server asked to back off (HTTP %d)%s", status, detail)
	}

	if status >= 500 {
		return Newf(ReasonInternal, "the server returned an internal error (HTTP %d)%s", status, detail)
	}

	return Newf(ReasonUnknown, "the server returned an unexpected status (HTTP %d)%s", status, detail)
}
