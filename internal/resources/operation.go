package resources

var (
	AdapterOperationCreate  = AdapterOperation{s: "create"}
	AdapterOperationFind    = AdapterOperation{s: "find"}
	AdapterOperationObserve = AdapterOperation{s: "observe"}
	AdapterOperationUpdate  = AdapterOperation{s: "update"}
	AdapterOperationDelete  = AdapterOperation{s: "delete"}
)

// AdapterOperation names each operation supported by the adapter.
type AdapterOperation struct{ s string }

func (a AdapterOperation) String() string {
	return a.s
}
