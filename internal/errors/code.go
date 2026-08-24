package errors

var CodeUnknown = Code{s: "Unknown"}

// Code is a fine-grained, machine-readable identifier for one failure, minted
// by the package that raises it — a condition reason, where Reason only
// carries the coarse class.
type Code struct{ s string }

func NewCode(s string) Code {
	return Code{s: s}
}

func (c Code) String() string {
	return c.s
}
