package cleanaccess

type cleanAccess struct{}

func (c *cleanAccess) Fetch(id string) (Record, error) { return Record{ID: id}, nil }
func (c *cleanAccess) Store(r Record) error            { return nil }

// recordFetcher is a LEGIT narrow accepted-interface: an unexported,
// consumer-side subset of CleanAccess (1 of its 2 methods). It must trip
// neither rule c (fewer methods than the contract — fails name-set equality)
// nor rule d (it is in the same package as the contract it narrows anyway).
type recordFetcher interface {
	Fetch(id string) (Record, error)
}
