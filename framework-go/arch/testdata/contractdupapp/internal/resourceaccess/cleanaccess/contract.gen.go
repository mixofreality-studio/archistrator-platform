package cleanaccess

// CleanAccess is the generated ResourceAccess port (contract surface).
type CleanAccess interface {
	Fetch(id string) (Record, error)
	Store(r Record) error
}

// Record is a generated contract value type.
type Record struct{ ID string }

// NewCleanAccess constructs the access (generated constructor).
func NewCleanAccess() CleanAccess { return &cleanAccess{} }
