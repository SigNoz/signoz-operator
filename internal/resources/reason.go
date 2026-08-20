package resources

var (
	ReasonSynced  = Reason{s: "Synced"}
	ReasonPending = Reason{s: "Pending"}
	ReasonCreated = Reason{s: "Created"}
	ReasonUpdated = Reason{s: "Updated"}

	// ReasonInvalidSpec is desired state the operator could not even render or
	// derive an identity from — a malformed jsonSpec. The resource's own fault.
	ReasonInvalidSpec = Reason{s: "InvalidSpec"}

	// ReasonRejected is a body the server refused (HTTP 400). The resource's own.
	ReasonRejected = Reason{s: "Rejected"}

	// ReasonAmbiguous is more than one candidate for an identity the operator
	// resolves itself. Never guessed.
	ReasonAmbiguous = Reason{s: "Ambiguous"}

	// ReasonSigNozResourceIDMismatch is a pinned signoz-resource-id naming no
	// object among those matching this resource's identity. The resource's own.
	ReasonSigNozResourceIDMismatch = Reason{s: "SigNozResourceIDMismatch"}

	// ReasonUnauthorized is a credential the server rejected (401/403).
	// Attributable to the provider config.
	ReasonUnauthorized = Reason{s: "Unauthorized"}

	// ReasonProviderConfigNotReady is a provider config that could not be
	// resolved into a connection. Attributable to the provider config.
	ReasonProviderConfigNotReady = Reason{s: "ProviderConfigNotReady"}

	// ReasonBackendError is a transient error from SigNoz (5xx/429).
	ReasonBackendError = Reason{s: "BackendError"}

	// ReasonBackendUnreachable is a dial, timeout or TLS failure reaching SigNoz.
	ReasonBackendUnreachable = Reason{s: "BackendUnreachable"}

	// ReasonSuspended is reconciliation paused by spec.suspend.
	ReasonSuspended = Reason{s: "Suspended"}
)

// Reasons are the machine-readable causes reported on conditions, shared across every mirrored kind.
type Reason struct{ s string }

func (r Reason) String() string {
	return r.s
}
