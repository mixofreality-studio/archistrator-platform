package conformance

import (
	"encoding/json"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// validEdgeKinds is the EdgeKind wire-name set methodcheck declares (edgeControlFlow,
// edgeGuardedFlow in methodcheck/project.go). methodcheck does not export the set, so it
// is mirrored here; an empty kind is not a member.
var validEdgeKinds = map[string]bool{"controlFlow": true, "guardedFlow": true}

// TestFixtures_ActivityEdgesCarryRecognizedKind walks every core use case activity
// diagram of every embedded fixture and fails, naming the use case and edge, when an edge
// kind is not a recognized EdgeKind wire name (the strict project codec refuses it).
func TestFixtures_ActivityEdgesCarryRecognizedKind(t *testing.T) {
	for _, name := range FixtureNames() {
		t.Run(name, func(t *testing.T) {
			raw, err := Fixture(name)
			if err != nil {
				t.Fatalf("Fixture(%q): %v", name, err)
			}
			p, ok, err := methodcheck.DecodeProject(raw)
			if err != nil || !ok {
				t.Fatalf("methodcheck.DecodeProject: ok=%v err=%v", ok, err)
			}
			slot, ok := p.Slots["4"] // CoreUseCases slot ordinal
			if !ok {
				return
			}
			var cu methodcheck.CoreUseCases
			if err := json.Unmarshal(slot.Model, &cu); err != nil {
				t.Fatalf("decode CoreUseCases: %v", err)
			}
			for _, d := range cu.Decisions {
				uc := d.UseCase
				if uc.Activity == nil {
					continue
				}
				for _, e := range uc.Activity.Edges {
					if !validEdgeKinds[e.Kind] {
						t.Errorf("use case %q (%s): edge %s→%s has unrecognized kind %q", uc.ID, uc.Name, e.From, e.To, e.Kind)
					}
				}
			}
		})
	}
}
