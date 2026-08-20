package resources

const (
	// AnnotationCreateAttempt records, before a create is issued, that the
	// operator is about to POST. It lives in metadata, not status, so it survives
	// a restore that drops status. Its value is the RFC3339 time of the attempt.
	// See docs/idempotency.md.
	AnnotationCreateAttempt string = "resources.signoz.io/create-attempt"

	// AnnotationAdoptExisting gates first-contact adoption of an object the
	// operator did not create. Absent it, a resource whose identity matches an
	// existing object goes Terminal rather than taking it over.
	AnnotationAdoptExisting = "resources.signoz.io/adopt-existing"

	// Finalizer keeps the custom resource until the operator has applied the
	// reclaim policy to the SigNoz object it mirrors.
	Finalizer = "resources.signoz.io/finalizer"
)
