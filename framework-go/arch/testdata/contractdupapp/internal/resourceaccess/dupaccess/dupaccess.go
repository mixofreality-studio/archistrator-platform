package dupaccess

type dupAccess struct{}

func (d *dupAccess) Fetch(id string) (Item, error) { return Item{ID: id}, nil }
func (d *dupAccess) Store(it Item) error           { return nil }

// HandDupAccess is a ROGUE exported hand-written interface: its method set
// (names + signatures, using the generated Item type) is a FULL, exact
// duplicate of the generated DupAccess contract declared in contract.gen.go —
// rule c (no-exported-hand-iface) must fire on it.
type HandDupAccess interface {
	Fetch(id string) (Item, error)
	Store(it Item) error
}

// MirrorItem is a local mirror of the generated Item type: same field shape,
// but a DIFFERENT named type declared here, not in contract.gen.go.
type MirrorItem struct{ ID string }

// ExportedMirrorTypeAccess is an EXPORTED near-miss interface that DOES reach
// ifaceMethodSetEqual as a rule-c candidate (exported, in dupaccess, a
// hasGeneratedFile package): same method NAMES and arity as the generated
// DupAccess contract (Fetch/Store), but its parameter/result type is
// MirrorItem — a local mirror of Item, not the generated type itself. This
// must NOT fire rule c: it proves types.Identical (structural signature
// equality), not just name-set/count, gates the match — if
// ifaceMethodSetEqual were regressed to a name/count-only compare, this
// interface would incorrectly start firing.
type ExportedMirrorTypeAccess interface {
	Fetch(id string) (MirrorItem, error)
	Store(it MirrorItem) error
}
