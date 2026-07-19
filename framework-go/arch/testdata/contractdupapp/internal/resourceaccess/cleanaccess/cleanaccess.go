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

// ExportedNarrowFetcher is an EXPORTED narrow subset of the generated
// CleanAccess contract (1 of its 2 methods, using the generated Record type
// itself — not a mirror). Because it is exported and lives in cleanaccess (a
// hasGeneratedFile package), it DOES reach ifaceMethodSetEqual as a rule-c
// candidate. It must NOT fire: a subset method-NAME-SET (1 vs. 2) fails exact
// name-set equality regardless of per-method signature equality. This proves
// the count/name-set gate is exercised through an exported candidate, not
// excluded purely by unexported visibility.
type ExportedNarrowFetcher interface {
	Fetch(id string) (Record, error)
}
