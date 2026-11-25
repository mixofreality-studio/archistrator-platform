package conformance

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
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
// then the construction records, as construction records them: the service contracts ONE AT A
// TIME (once in key order, once in reverse), then the UI designs, the component test plans and the
// acceptance plan. Nobody records a Client's contract: a machine client's (rest, mcpTools) is
// derived in the mutation that records the Manager behind it, and a browser surface's (web, mcpApp)
// when its UI design is recorded, so the replay adds each in that step.
//
// After each step the prefix loads under Tier 0 (projectmodel.Load, which the construction record
// verbs refuse on and every generator loads through), validates with zero Error findings under the
// severity the record verb applies, and, at a step whose artifact a generator reads, passes Tier 1
// admission with no Local problem: what the write verbs would do at that step. Tier 0 runs at the
// design slots too: a design whose deployment binds a component is a prefix every later contract
// record must load, so a slot that Tier 0 refuses would refuse the first contract (greenfield's
// slot 6 did, with PM-DEPLOY-REF, until a reference to a System component whose contract is still
// to come became pending).

// replayStep is one recorded artifact: what the prefix gains and the kind Tier 1 admits it as (""
// when no generator reads it).
type replayStep struct {
	name  string
	admit appgen.ArtifactKind
	add   func(prefix map[string]any)
}

// contractOrder is the order a replay records the service contracts in.
type contractOrder struct {
	name  string
	order func(keys []string) []string
}

// replayContractOrders are the two recording orders every replay runs: key order and its reverse.
var replayContractOrders = []contractOrder{
	{"key order", func(keys []string) []string { return slices.Sorted(slices.Values(keys)) }},
	{"reverse key order", func(keys []string) []string {
		o := slices.Sorted(slices.Values(keys))
		slices.Reverse(o)
		return o
	}},
}

// slotAdmitKinds is the Tier 1 kind of the slots a generator reads, by kind ordinal.
var slotAdmitKinds = map[int]appgen.ArtifactKind{
	4: appgen.KindCoreUseCases,
	5: appgen.KindSystem,
	6: appgen.KindOperationalConcepts,
}

// replaySystem is the part of the System the replay schedules contracts by.
type replaySystem struct {
	Components []struct {
		ID          string `json:"id"`
		Kind        string `json:"kind"`
		Surface     string `json:"surface"`
		ContractKey string `json:"contractKey"`
		BuildStatus string `json:"buildStatus"`
	} `json:"components"`
	Relationships []struct {
		From string `json:"from"`
		To   string `json:"to"`
	} `json:"relationships"`
}

// systemOf is the System among slots (kind 5), the zero System when there is none.
func systemOf(t *testing.T, slots map[string]any) replaySystem {
	t.Helper()
	var sys replaySystem
	for _, s := range slots {
		slot, _ := s.(map[string]any)
		if k, _ := slot["kind"].(float64); k != 5 {
			continue
		}
		b, err := json.Marshal(slot["model"])
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &sys); err != nil {
			t.Fatal(err)
		}
	}
	return sys
}

// contractKeyOf is the key a component's contract is filed under: its contractKey, else
// lowerCamel(id) (the app's projectstate.ContractKeyFor).
func contractKeyOf(id, contractKey string) string {
	if contractKey != "" {
		return contractKey
	}
	parts := strings.Split(id, "-")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

// contractSchedule splits the contract keys into those an agent records (the non-Client ones), the
// machine-client contracts derived with a Manager's (by that Manager's key: the last Manager the
// client calls, in order), and the browser-surface contracts derived with the UI design.
func contractSchedule(t *testing.T, sys replaySystem, keys []string, co contractOrder) (recorded []string, withManager map[string][]string, withUIDesign []string) {
	t.Helper()
	have := map[string]bool{}
	for _, k := range keys {
		have[k] = true
	}
	keyOfID := map[string]string{}
	kindOfID := map[string]string{}
	clientSurface := map[string]string{} // contract key -> surface, for Clients
	for _, c := range sys.Components {
		key := contractKeyOf(c.ID, c.ContractKey)
		keyOfID[c.ID] = key
		kindOfID[c.ID] = strings.ToLower(c.Kind)
		if strings.EqualFold(c.Kind, "client") {
			clientSurface[key] = c.Surface
		}
	}
	var agent []string
	for _, k := range keys {
		if _, client := clientSurface[k]; !client {
			agent = append(agent, k)
		}
	}
	recorded = co.order(agent)
	position := map[string]int{}
	for i, k := range recorded {
		position[k] = i
	}
	withManager = map[string][]string{}
	for _, c := range sys.Components {
		key := keyOfID[c.ID]
		surface, client := clientSurface[key]
		if !client || !have[key] {
			continue
		}
		switch surface {
		case "web", "mcpApp":
			withUIDesign = append(withUIDesign, key)
		case "rest", "mcpTools":
			last := ""
			for _, r := range sys.Relationships {
				m := keyOfID[r.To]
				if r.From != c.ID || kindOfID[r.To] != "manager" || !have[m] {
					continue
				}
				if last == "" || position[m] > position[last] {
					last = m
				}
			}
			if last == "" {
				t.Fatalf("machine client %s has a contract but calls no contracted Manager: the replay cannot tell when it is derived", c.ID)
			}
			withManager[last] = append(withManager[last], key)
		default:
			t.Fatalf("client %s has surface %q: the replay knows when a web, mcpApp, rest or mcpTools contract is recorded; extend it", c.ID, surface)
		}
	}
	return recorded, withManager, withUIDesign
}

// addContracts files the named contracts into the prefix's serviceContracts.
func addContracts(p map[string]any, contracts map[string]any, keys ...string) {
	sc, _ := p["serviceContracts"].(map[string]any)
	if sc == nil {
		sc = map[string]any{}
		p["serviceContracts"] = sc
	}
	for _, k := range keys {
		sc[k] = contracts[k]
	}
}

// replay splits a fixture into its empty envelope and its steps in Method order, recording the
// service contracts one at a time in co's order.
func replay(t *testing.T, raw []byte, co contractOrder) (map[string]any, []replayStep) {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	slots, _ := doc["slots"].(map[string]any)
	contracts, _ := doc["serviceContracts"].(map[string]any)
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
	recorded, withManager, withUIDesign := contractSchedule(t, systemOf(t, slots), slices.Collect(maps.Keys(contracts)), co)
	for _, key := range recorded {
		keys := append([]string{key}, withManager[key]...)
		steps = append(steps, replayStep{name: "serviceContracts." + key, admit: appgen.KindServiceContract, add: func(p map[string]any) {
			addContracts(p, contracts, keys...)
		}})
	}
	if _, ok := artifacts["uiDesign"]; !ok && len(withUIDesign) > 0 {
		t.Fatalf("browser surfaces %v have contracts but the fixture records no UI design to derive them with", withUIDesign)
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
		var derived []string
		if key == "uiDesign" {
			derived = withUIDesign
		}
		steps = append(steps, replayStep{name: "phaseArtifacts." + key, admit: rec.admit, add: func(p map[string]any) {
			pa, _ := p["phaseArtifacts"].(map[string]any)
			if pa == nil {
				pa = map[string]any{}
				p["phaseArtifacts"] = pa
			}
			pa[key] = v
			addContracts(p, contracts, derived...)
		}})
		delete(artifacts, key)
	}
	for key := range artifacts {
		t.Fatalf("phaseArtifacts.%s is outside the Method order the replay knows: extend it", key)
	}
	return envelope, steps
}

// gateFindingsOf is the verdict of the gate a built app runs (scaffold aiarch_arch_test.go.tmpl:
// methodcheck.Check over this spec, no FindingPolicy) on the prefix's design.
func gateFindingsOf(prefix []byte) ([]methodcheck.Finding, error) {
	p, ok, err := methodcheck.DecodeProject(prefix)
	if err != nil {
		return nil, fmt.Errorf("methodcheck.DecodeProject: %w", err)
	}
	if !ok {
		return nil, nil
	}
	findings, err := methodcheck.GateFindings(p, methodcheck.ProjectSpec{SupportedSurfaces: appgen.SupportedSurfaces()})
	if err != nil {
		return nil, fmt.Errorf("methodcheck.GateFindings: %w", err)
	}
	return findings, nil
}

// stepProblems validates the prefix as the write verbs would after recording the step: Tier 0
// (the project model every generator loads), the methodcheck design rules as the gate every built
// app runs judges them (GateFindings with the template's spec and no FindingPolicy: the
// contracts-pending window is methodcheck's own), then Tier 1 admission of the step's kind (Local
// problems).
func stepProblems(prefix []byte, st replayStep) ([]string, error) {
	if _, err := projectmodel.Load(prefix); err != nil {
		return nil, fmt.Errorf("tier 0 projectmodel.Load: %w", err)
	}
	findings, err := gateFindingsOf(prefix)
	if err != nil {
		return nil, err
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

// Every slot of every fixture validates with only its upstream slots recorded, and so does every
// contract, recorded one at a time in key order and in reverse.
func TestSlotPrefixReplay_EverySlotPassesWithOnlyItsUpstream(t *testing.T) {
	names := FixtureNames()
	if len(names) == 0 {
		t.Fatal("no conformance fixtures embedded")
	}
	for _, name := range names {
		for _, co := range replayContractOrders {
			t.Run(name+"/"+co.name, func(t *testing.T) {
				raw, err := Fixture(name)
				if err != nil {
					t.Fatal(err)
				}
				prefix, steps := replay(t, raw, co)
				contractSteps := 0
				for _, st := range steps {
					if strings.HasPrefix(st.name, "serviceContracts.") {
						contractSteps++
					}
				}
				if len(steps) < 2 || contractSteps < 2 {
					t.Fatalf("%d replay step(s), %d of them contracts: the replay is vacuous", len(steps), contractSteps)
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
				// The advisory window hides nothing at the end: a whole replayed fixture with every
				// contract-bearing component contracted holds at full severity. A fixture that records
				// no contract for some component (greenfield's web-client: no UI design) still reports
				// every Error problem, and the uncontracted ids are logged.
				b, err := json.Marshal(prefix)
				if err != nil {
					t.Fatal(err)
				}
				if p, ok, derr := methodcheck.DecodeProject(b); derr != nil || !ok {
					t.Fatalf("decode the whole fixture: %v (ok=%v)", derr, ok)
				} else if pending := methodcheck.UncontractedComponents(p); len(pending) != 0 {
					if name == "todomvc" {
						t.Errorf("the whole todomvc fixture leaves %v without a contract: the full-severity end state is unasserted", pending)
					}
					t.Logf("the whole fixture still leaves %v without a contract", pending)
				}
				problems, err := stepProblems(b, replayStep{name: "the whole fixture"})
				if err != nil {
					t.Fatal(err)
				}
				for _, p := range problems {
					t.Errorf("the whole fixture at full severity: %s", p)
				}
			})
		}
	}
}

// The replay schedules a Client's contract where construction derives it, never as a record of
// its own: todomvc's machine clients ride with listManager, its browser surfaces with the UI design.
func TestSlotPrefixReplay_ClientContractsAreDerivedNotRecorded(t *testing.T) {
	raw, err := Fixture("todomvc")
	if err != nil {
		t.Fatal(err)
	}
	_, steps := replay(t, raw, replayContractOrders[0])
	var names []string
	for _, st := range steps {
		if strings.HasPrefix(st.name, "serviceContracts.") {
			names = append(names, strings.TrimPrefix(st.name, "serviceContracts."))
		}
	}
	want := []string{"accessGrantingEngine", "listManager", "listRulingEngine", "messageBus", "todoListAccess"}
	if !slices.Equal(names, want) {
		t.Fatalf("contract record steps = %v, want %v", names, want)
	}
	prefix := map[string]any{}
	for _, st := range steps {
		if st.name == "serviceContracts.listManager" {
			st.add(prefix)
		}
	}
	got := slices.Sorted(maps.Keys(prefix["serviceContracts"].(map[string]any)))
	if !slices.Equal(got, []string{"listManager", "todoApi", "todoMcpServer"}) {
		t.Fatalf("the listManager step records %v, want listManager with its machine clients todoApi and todoMcpServer", got)
	}
}

// The record verb and the gate judge a design alike: at every prefix of every replay, the Error
// findings ValidateProjectJSON reports (what the record verb applies) are the ones the gate's
// GateFindings reports. The test is not vacuous: some prefix still owes a contract and holds one of
// the three contract-set rules as a Warning.
func TestSlotPrefixReplay_RecordVerbVerdictIsTheGateVerdict(t *testing.T) {
	raw, err := Fixture("todomvc")
	if err != nil {
		t.Fatal(err)
	}
	contractRules := map[methodcheck.RuleID]bool{"SCN-UNADDRESSED": true, "SCN-ACTOR-PATH-CLIENT": true, "TP-CLIENT-CONTRACTED": true}
	errorsOf := func(fs []methodcheck.Finding) []string {
		var out []string
		for _, f := range fs {
			if f.Severity == methodcheck.SeverityError {
				out = append(out, string(f.RuleID)+": "+f.Message)
			}
		}
		slices.Sort(out)
		return out
	}
	advisory := false
	for _, co := range replayContractOrders {
		prefix, steps := replay(t, raw, co)
		for _, st := range steps {
			st.add(prefix)
			b, err := json.Marshal(prefix)
			if err != nil {
				t.Fatal(err)
			}
			verb, err := methodcheck.ValidateProjectJSON(b, methodcheck.ValidateOptions{SupportedSurfaces: appgen.SupportedSurfaces()})
			if err != nil {
				t.Fatalf("%s, after %s: record verb: %v", co.name, st.name, err)
			}
			gate, err := gateFindingsOf(b)
			if err != nil {
				t.Fatalf("%s, after %s: %v", co.name, st.name, err)
			}
			if v, g := errorsOf(verb), errorsOf(gate); !slices.Equal(v, g) {
				t.Errorf("%s, after %s: the record verb's Errors %v differ from the gate's %v", co.name, st.name, v, g)
			}
			p, _, _ := methodcheck.DecodeProject(b)
			if len(methodcheck.UncontractedComponents(p)) == 0 {
				continue
			}
			for _, f := range gate {
				if f.Severity == methodcheck.SeverityWarning && contractRules[f.RuleID] {
					advisory = true
				}
			}
		}
	}
	if !advisory {
		t.Error("no prefix owed a contract and held SCN-UNADDRESSED, SCN-ACTOR-PATH-CLIENT or TP-CLIENT-CONTRACTED as a Warning: the comparison is vacuous")
	}
}
