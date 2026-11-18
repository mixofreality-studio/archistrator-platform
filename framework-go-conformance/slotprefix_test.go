package conformance

import (
	"encoding/json"
	"fmt"
	"strconv"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/appgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// slot-prefix replay (RUN-025). A design is recorded one slot at a time, and every slot is
// validated with only its upstream slots recorded: the drafter of the core use cases has no System
// and no contract to satisfy. A rule that judges an artifact the Method has not reached yet refuses
// every honest draft of the slot being recorded, and a whole-fixture check never sees it, because
// the whole fixture has every artifact. RUN-025's coreUseCases drafter met exactly that: the
// scenario-derivation rules asked each actor's input to reach a contracted Client, and could not
// be satisfied until construction.
//
// So each fixture is replayed in Method order: the slots by kind ordinal (Phase 1, then Phase 2),
// then the construction records (the service contracts, the UI designs, the component test plans,
// the acceptance plan). After each step the prefix validates with zero Error findings, and a step
// whose artifact a generator reads passes Tier 1 admission with no Local problem: what the write
// verbs would do at that step. A construction record also loads under Tier 0 (projectmodel.Load),
// which the construction record verbs refuse on; the design-slot verbs do not run it, so neither
// does the replay at a design slot.

// replayStep is one recorded artifact: what the prefix gains, the kind Tier 1 admits it as ("" when
// no generator reads it), and whether its verb gates on Tier 0.
type replayStep struct {
	name  string
	admit appgen.ArtifactKind
	tier0 bool
	add   func(prefix map[string]any)
}

// slotAdmitKinds is the Tier 1 kind of the slots a generator reads, by kind ordinal.
var slotAdmitKinds = map[int]appgen.ArtifactKind{
	4: appgen.KindCoreUseCases,
	5: appgen.KindSystem,
	6: appgen.KindOperationalConcepts,
}

// replay splits a fixture into its empty envelope and its steps in Method order.
func replay(t *testing.T, raw []byte) (map[string]any, []replayStep) {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	slots, _ := doc["slots"].(map[string]any)
	contracts, hasContracts := doc["serviceContracts"]
	artifacts, _ := doc["phaseArtifacts"].(map[string]any)
	envelope := map[string]any{}
	for k, v := range doc {
		switch k {
		case "slots", "serviceContracts", "phaseArtifacts":
		default:
			envelope[k] = v
		}
	}
	envelope["slots"] = map[string]any{}

	byKind := map[int]string{}
	for key, s := range slots {
		k, ok := s.(map[string]any)["kind"].(float64)
		if !ok {
			t.Fatalf("slot %s has no kind", key)
		}
		byKind[int(k)] = key
	}
	var steps []replayStep
	for kind := 0; kind <= 16; kind++ {
		key, ok := byKind[kind]
		if !ok {
			continue
		}
		slot := slots[key]
		steps = append(steps, replayStep{name: "slot " + strconv.Itoa(kind), admit: slotAdmitKinds[kind], add: func(p map[string]any) {
			p["slots"].(map[string]any)[key] = slot
		}})
		delete(byKind, kind)
	}
	for kind := range byKind {
		t.Fatalf("slot kind %d is outside the Method order the replay knows (0-16): extend it", kind)
	}
	if hasContracts {
		steps = append(steps, replayStep{name: "serviceContracts", admit: appgen.KindServiceContract, tier0: true, add: func(p map[string]any) {
			p["serviceContracts"] = contracts
		}})
	}
	for _, rec := range []struct {
		key   string
		admit appgen.ArtifactKind
	}{{"uiDesign", appgen.KindUIDesign}, {"testPlan", appgen.KindTestPlan}, {"acceptancePlan", appgen.KindAcceptancePlan}} {
		v, ok := artifacts[rec.key]
		if !ok {
			continue
		}
		key := rec.key
		steps = append(steps, replayStep{name: "phaseArtifacts." + key, admit: rec.admit, tier0: true, add: func(p map[string]any) {
			pa, _ := p["phaseArtifacts"].(map[string]any)
			if pa == nil {
				pa = map[string]any{}
				p["phaseArtifacts"] = pa
			}
			pa[key] = v
		}})
		delete(artifacts, key)
	}
	for key := range artifacts {
		t.Fatalf("phaseArtifacts.%s is outside the Method order the replay knows: extend it", key)
	}
	return envelope, steps
}

// stepProblems validates the prefix as the write verbs would after recording the step: Tier 0
// (the project model every generator loads) where the verb gates on it, the methodcheck design
// rules (Error findings), then Tier 1 admission of the step's kind (Local problems).
func stepProblems(prefix []byte, st replayStep) ([]string, error) {
	if st.tier0 {
		if _, err := projectmodel.Load(prefix); err != nil {
			return nil, fmt.Errorf("tier 0 projectmodel.Load: %w", err)
		}
	}
	findings, err := methodcheck.ValidateProjectJSON(prefix, methodcheck.ValidateOptions{SupportedSurfaces: appgen.SupportedSurfaces()})
	if err != nil {
		return nil, fmt.Errorf("methodcheck.ValidateProjectJSON: %w", err)
	}
	var out []string
	for _, f := range findings {
		if f.Severity == methodcheck.SeverityError {
			out = append(out, fmt.Sprintf("%s: %s", f.RuleID, f.Message))
		}
	}
	if st.admit == "" {
		return out, nil
	}
	in := appgen.Input{Project: prefix, ModulePath: "example.com/app", ModuleDir: "server", Config: appgen.DefaultConfig()}
	for _, p := range appgen.AdmitTier1(in, st.admit) {
		if _, local := p.Severity.(appgen.Local); local {
			out = append(out, fmt.Sprintf("tier 1 %s %s: %s", p.Consumer, p.Path, p.Message))
		}
	}
	return out, nil
}

// Every slot of every fixture validates with only its upstream slots recorded.
func TestSlotPrefixReplay_EverySlotPassesWithOnlyItsUpstream(t *testing.T) {
	names := FixtureNames()
	if len(names) == 0 {
		t.Fatal("no conformance fixtures embedded")
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			raw, err := Fixture(name)
			if err != nil {
				t.Fatal(err)
			}
			prefix, steps := replay(t, raw)
			if len(steps) < 2 {
				t.Fatalf("%d replay step(s): the replay is vacuous", len(steps))
			}
			for _, st := range steps {
				st.add(prefix)
				b, err := json.Marshal(prefix)
				if err != nil {
					t.Fatal(err)
				}
				problems, err := stepProblems(b, st)
				if err != nil {
					t.Errorf("after %s: %v", st.name, err)
					continue
				}
				for _, p := range problems {
					t.Errorf("after %s: %s", st.name, p)
				}
			}
		})
	}
}
