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
