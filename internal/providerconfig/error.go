package providerconfig

type ResolverError struct {
	Reason  Reason
	Message string
	cause   error
}

func NewResolverError(reason Reason, message string, cause error) *ResolverError {
	return &ResolverError{Reason: reason, Message: message, cause: cause}
}

func (e *ResolverError) Error() string {
	return e.Message
}

func (e *ResolverError) Unwrap() error {
	return e.cause
}
