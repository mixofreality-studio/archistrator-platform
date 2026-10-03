package scenario

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

func load(t *testing.T, name string) Input {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var in Input
	if err := json.Unmarshal(raw, &in); err != nil {
		t.Fatal(err)
	}
	return in
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	p := filepath.Join("testdata", name+".golden.json")
	if *update {
		if err := os.WriteFile(p, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(got) {
		t.Fatalf("golden mismatch for %s\n--- want\n%s\n--- got\n%s", name, want, got)
	}
}

func TestDerive_OrderingGolden(t *testing.T) {
	s, err := Derive(load(t, "ordering"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := CanonicalJSON(s)
	golden(t, "ordering", b)
}

func TestDerive_TwoEntries(t *testing.T) {
	s, err := Derive(load(t, "two_entries"))
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 3 {
		t.Fatalf("want 3 scenarios (start family, tick family, tick loop), got %d", len(s))
	}
	b, _ := CanonicalJSON(s)
	golden(t, "two_entries", b)
}

// Out-edges sort ascending by (To, Guard) (spec §2.3), so "reject" precedes
// "send-invoice" and the reject branch is order-P1 = s[0].
func TestDerive_UnguardedBranch(t *testing.T) {
	in := load(t, "ordering")
	in.Diagrams[0].Edges[3].Guard = "" // accepted -> reject loses its guard
	s, err := Derive(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 2 || s[0].ID != "order-P1" || len(s[0].Guards) != 1 || s[0].Guards[0].Guard != "" {
		t.Fatalf("unguarded branch must still enumerate with empty guard: %+v", s)
	}
}

func TestDerive_UnresolvedOpIsEmptyNotError(t *testing.T) {
	in := load(t, "ordering")
	in.Views[0].Steps[0].Calls[0].Label = "Place the order please"
	s, err := Derive(in)
	if err != nil {
		t.Fatal(err)
	}
	if s[0].Stimuli[0].Input.Op != "" {
		t.Fatalf("want unresolved op to be empty, got %q", s[0].Stimuli[0].Input.Op)
	}
}

func TestDerive_IsDeterministic(t *testing.T) {
	in := load(t, "ordering")
	f1, _ := Fingerprint(must(Derive(in)))
	// shuffle node order; ids must not change
	in.Diagrams[0].Nodes[0], in.Diagrams[0].Nodes[5] = in.Diagrams[0].Nodes[5], in.Diagrams[0].Nodes[0]
	f2, _ := Fingerprint(must(Derive(in)))
	if f1 != f2 {
		t.Fatal("node declaration order changed the fingerprint")
	}
}

// The accepted branch (where "pay" lives) is order-P2 under the (To, Guard) ordering.
func TestForComponent(t *testing.T) {
	s := must(Derive(load(t, "ordering")))
	b := ForComponent(s, "billing-manager")
	if len(b) != 1 || b[0].ID != "order-P2" || len(b[0].Stimuli) != 1 || b[0].Stimuli[0].Seq != 1 || b[0].Stimuli[0].Node != "pay" {
		t.Fatalf("projection wrong: %+v", b)
	}
}

func must(s []Scenario, err error) []Scenario {
	if err != nil {
		panic(err)
	}
	return s
}
