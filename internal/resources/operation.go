package resources

var (
	AdapterOperationCreate  = AdapterOperation{s: "create"}
	AdapterOperationFind    = AdapterOperation{s: "find"}
	AdapterOperationObserve = AdapterOperation{s: "observe"}
	AdapterOperationUpdate  = AdapterOperation{s: "update"}
	AdapterOperationDelete  = AdapterOperation{s: "delete"}
)

type AdapterOperation struct{ s string }

func (a AdapterOperation) String() string {
	return a.s
}
