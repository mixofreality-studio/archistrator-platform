package dupaccess

// DupAccess is the generated ResourceAccess port (contract surface).
type DupAccess interface {
	Fetch(id string) (Item, error)
	Store(it Item) error
}

// Item is a generated contract value type.
type Item struct{ ID string }

// NewDupAccess constructs the access (generated constructor).
func NewDupAccess() DupAccess { return &dupAccess{} }
