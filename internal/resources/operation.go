package resources

var (
	AdapterOperationCreate = AdapterOperation{s: "create"}
	AdapterOperationFind   = AdapterOperation{s: "find"}
	AdapterOperationRead   = AdapterOperation{s: "read"}
	AdapterOperationUpdate = AdapterOperation{s: "update"}
	AdapterOperationDelete = AdapterOperation{s: "delete"}
)

type AdapterOperation struct{ s string }

func (a AdapterOperation) String() string {
	return a.s
}
