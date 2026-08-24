package errors

import (
	"errors"
	"fmt"
)

// Base is the error propagated across the codebase, modeled on apimachinery's
// StatusError and SigNoz's pkg/errors.
type Base struct {
	reason  Reason
	code    Code
	message string
	cause   error
}

func (b *Base) Error() string {
	if b.cause != nil {
		return b.message + ": " + b.cause.Error()
	}

	return b.message
}

// Reason is the error's reason
func (b *Base) Reason() Reason {
	return b.reason
}

// Message is the error's own message, without the wrapped cause.
func (b *Base) Message() string {
	return b.message
}

func (b *Base) Code() Code {
	return b.code
}

// Unwrap exposes the wrapped cause so errors.Is and errors.As can walk the chain.
func (b *Base) Unwrap() error {
	return b.cause
}

// WithCode returns a copy carrying code.
func (b *Base) WithCode(code Code) *Base {
	copied := *b
	copied.code = code

	return &copied
}

func New(reason Reason, message string) *Base {
	return &Base{reason: reason, code: CodeUnknown, message: message}
}

func Newf(reason Reason, format string, args ...any) *Base {
	return &Base{reason: reason, code: CodeUnknown, message: fmt.Sprintf(format, args...)}
}

func Wrap(cause error, reason Reason, message string) *Base {
	return &Base{reason: reason, code: CodeUnknown, message: message, cause: cause}
}

func Wrapf(cause error, reason Reason, format string, args ...any) *Base {
	return &Base{reason: reason, code: CodeUnknown, message: fmt.Sprintf(format, args...), cause: cause}
}

// ReasonForError returns the reason of the first Base in the chain, or
// ReasonUnknown when there is none.
func ReasonForError(err error) Reason {
	var base *Base
	if errors.As(err, &base) {
		return base.reason
	}

	return ReasonUnknown
}

func IsNotFound(err error) bool {
	return ReasonForError(err) == ReasonNotFound
}

func IsAlreadyExists(err error) bool {
	return ReasonForError(err) == ReasonAlreadyExists
}

func IsUnauthorized(err error) bool {
	return ReasonForError(err) == ReasonUnauthorized
}

func IsForbidden(err error) bool {
	return ReasonForError(err) == ReasonForbidden
}

func IsUnreachable(err error) bool {
	return ReasonForError(err) == ReasonUnreachable
}

// IsRetryable reports whether a retry may fix the failure; an error carrying
// no Base is retryable, its outcome is unknown.
func IsRetryable(err error) bool {
	return err != nil && ReasonForError(err).Retryable()
}

// Is, As and Join wrap the stdlib so call sites need only this package.
func Is(err, target error) bool {
	return errors.Is(err, target)
}

func As(err error, target any) bool {
	return errors.As(err, target)
}

func Join(errs ...error) error {
	return errors.Join(errs...)
}
