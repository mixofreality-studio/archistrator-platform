package scenario

import (
	"math/rand"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// The clients of the todomvc fixtures, spelled as their contract keys.
var acceptanceClients = []string{"todoWebApp", "todoMcpServer", "todoApi"}

func deriveAll(t *testing.T, name string) []Scenario {
	t.Helper()
	ss, err := Derive(load(t, name))
	if err != nil {
		t.Fatal(err)
	}
	return ss
}

func variantByID(t *testing.T, vs []Scenario, id string) Scenario {
	t.Helper()
	for _, v := range vs {
		if v.ID == id {
			return v
		}
	}
	t.Fatalf("variant %q not in %v", id, variantIDs(vs))
	return Scenario{}
}

func variantIDs(vs []Scenario) []string {
	var ids []string
	for _, v := range vs {
		ids = append(ids, v.ID)
	}
	return ids
}

func reasonsOf(rs []Residual, id string) []ResidualReason {
	var out []ResidualReason
	for _, r := range rs {
		if r.Scenario == id {
			out = append(out, r.Reason)
		}
	}
	return out
}

func hasReason(rs []Residual, id string, want ResidualReason) bool {
	for _, r := range reasonsOf(rs, id) {
		if reflect.DeepEqual(r, want) {
			return true
		}
	}
	return false
}

func TestForAcceptance_OneVariantPerCoverClient(t *testing.T) {
	vs := ForAcceptance(deriveAll(t, "acceptance_crossactor"), acceptanceClients)
	for _, id := range []string{"work-the-list-P1@todoApi", "work-the-list-P1@todoMcpServer", "work-the-list-P1@todoWebApp"} {
		v := variantByID(t, vs, id)
		cover := id[strings.LastIndex(id, "@")+1:]
		var atN1a []Stimulus
		for _, st := range v.Stimuli {
			if st.Input.Kind == "call" {
				t.Errorf("%s keeps a component-to-component stimulus %s/%s", id, st.Node, st.Input.Op)
			}
			if st.Node == "n1a" {
				atN1a = append(atN1a, st)
				if st.Input.To != Normalize(cover) {
					t.Errorf("%s keeps an n1a stimulus into %s, want only %s", id, st.Input.To, Normalize(cover))
				}
			}
		}
		if len(atN1a) == 0 {
			t.Fatalf("%s has no stimulus at n1a", id)
		}
		// One stimulus per op of the client: the alternatives into the other clients are dropped.
		ops := map[string]bool{}
		for _, st := range atN1a {
			if ops[st.Input.Op] {
				t.Errorf("%s has two n1a stimuli for op %s", id, st.Input.Op)
			}
			ops[st.Input.Op] = true
		}
		for i, st := range v.Stimuli {
			if st.Seq != i+1 {
				t.Errorf("%s seq not renumbered: stimulus %d has Seq %d", id, i, st.Seq)
			}
		}
	}
	api := variantByID(t, vs, "work-the-list-P1@todoApi")
	st := api.Stimuli[0]
	if st.Input.From != "team-agent" {
		t.Errorf("@todoApi first stimulus From = %q, want team-agent", st.Input.From)
	}
	var seen []string
	for _, o := range st.ActorOutcome.ExpectedOutputs {
		seen = append(seen, o.Node)
	}
	slices.Sort(seen)
	if !slices.Equal(seen, []string{"out-team-agent", "out-team-member"}) {
		t.Errorf("@todoApi ActorOutcome outputs = %v, want out-team-member and out-team-agent", seen)
	}
	if !slices.IsSorted(variantIDs(vs)) {
		t.Errorf("variants not sorted by id: %v", variantIDs(vs))
	}
}

func TestForAcceptance_CrossClientIsResidual(t *testing.T) {
	all := deriveAll(t, "acceptance_crossactor")
	rs := AcceptanceResidual(all, acceptanceClients)
	if !hasReason(rs, "work-the-list-P1@todoApi", LaterOutcome{OutcomeNode: "out-team-member", AfterStimulus: "n1a"}) {
		t.Errorf("@todoApi lacks LaterOutcome{out-team-member, n1a}: %v", rs)
	}
	if !hasReason(rs, "work-the-list-P1@todoWebApp", LaterOutcome{OutcomeNode: "out-team-agent", AfterStimulus: "n1a"}) {
		t.Errorf("@todoWebApp lacks LaterOutcome{out-team-agent, n1a}: %v", rs)
	}

	// A hand-built path whose actor stimuli at n1 target clientA and at n3 clientB leaves the
	// client mid-way, whichever client covers it.
	hand := []Scenario{{
		ID: "walk-P1",
		Stimuli: []Stimulus{
			{Seq: 1, Node: "n1", Input: InputEv{Kind: "action", From: "member", To: "clienta", Op: "Open"}},
			{Seq: 2, Node: "n2", Input: InputEv{Kind: "call", From: "client-a", To: "mgr", Op: "Open"}},
			{Seq: 3, Node: "n3", Input: InputEv{Kind: "action", From: "member", To: "clientb", Op: "Close"}},
		},
	}}
	hrs := AcceptanceResidual(hand, []string{"clientA", "clientB"})
	want := CrossClient{Clients: []string{"clientA", "clientB"}}
	for _, id := range []string{"walk-P1@clientA", "walk-P1@clientB"} {
		if !hasReason(hrs, id, want) {
			t.Errorf("%s lacks %v: %v", id, want, hrs)
		}
	}
}

func TestForAcceptance_LaterStimulusOutcomeIsResidual(t *testing.T) {
	all := deriveAll(t, "acceptance_event")
	clients := []string{"todoWebApp"}
	vs := ForAcceptance(all, clients)
	v := variantByID(t, vs, "tidy-nightly-P1@todoWebApp")
	if first := v.Stimuli[0]; first.Node != "t1" || first.Input.Kind != "timeEvent" || first.Input.To != Normalize("listManager") {
		t.Fatalf("first stimulus = %+v, want the timeEvent into listManager", first.Input)
	}
	rs := AcceptanceResidual(all, clients)
	id := "tidy-nightly-P1@todoWebApp"
	if !hasReason(rs, id, EventEntered{Node: "t1"}) {
		t.Errorf("lacks EventEntered{t1}: %v", rs)
	}
	if !hasReason(rs, id, LaterOutcome{OutcomeNode: "out-member", AfterStimulus: "t1"}) {
		t.Errorf("lacks LaterOutcome{out-member, t1}: %v", rs)
	}
	// The outcome of the member's own later stimulus into the client is the web projection's.
	if hasReason(rs, id, LaterOutcome{OutcomeNode: "out-member-back", AfterStimulus: "n3"}) {
		t.Errorf("the member's own outcome after n3 must not be residual: %v", rs)
	}
}

func TestForAcceptance_CoveredIsNotResidual(t *testing.T) {
	all := deriveAll(t, "acceptance_covered")
	vs := ForAcceptance(all, acceptanceClients)
	for _, id := range []string{
		"come-back-to-an-intact-list-P1@todoWebApp",
		"come-back-to-an-intact-list-P1@todoMcpServer",
		"drive-the-list-from-agent-tooling-P1@todoMcpServer",
		"drive-the-list-from-agent-tooling-P1@todoApi",
	} {
		variantByID(t, vs, id)
	}
	if rs := AcceptanceResidual(all, acceptanceClients); len(rs) != 0 {
		t.Errorf("covered paths are residual: %v", rs)
	}
}

func TestForAcceptance_NoActorStimulusNoVariant(t *testing.T) {
	only := []Scenario{{ID: "internal-P1", Stimuli: []Stimulus{
		{Seq: 1, Node: "n1", Input: InputEv{Kind: "call", From: "a", To: "b"}},
	}}}
	if vs := ForAcceptance(only, []string{"b"}); len(vs) != 0 {
		t.Errorf("variants for a scenario with no actor stimulus: %v", variantIDs(vs))
	}
}

func TestAcceptanceResidual_IsDeterministic(t *testing.T) {
	all := deriveAll(t, "acceptance_crossactor")
	all = append(all, deriveAll(t, "acceptance_covered")...)
	all = append(all, deriveAll(t, "acceptance_event")...)
	clients := append([]string(nil), acceptanceClients...)
	want := AcceptanceResidual(all, clients)
	if len(want) == 0 {
		t.Fatal("empty residual")
	}
	rng := rand.New(rand.NewSource(7))
	for range 5 {
		shuffled := append([]Scenario(nil), all...)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		cs := append([]string(nil), clients...)
		rng.Shuffle(len(cs), func(i, j int) { cs[i], cs[j] = cs[j], cs[i] })
		if got := AcceptanceResidual(shuffled, cs); !reflect.DeepEqual(got, want) {
			t.Fatalf("residual depends on input order:\n got %v\nwant %v", got, want)
		}
	}
}

func TestIsResidual(t *testing.T) {
	rs := []Residual{{Scenario: "a@x", Reason: EventEntered{Node: "t"}}, {Scenario: "a@x", Reason: CrossClient{Clients: []string{"x", "y"}}}}
	if !IsResidual(rs, "a@x") || IsResidual(rs, "a@y") {
		t.Errorf("IsResidual wrong over %v", rs)
	}
}

func TestResidualReason_Describe(t *testing.T) {
	for _, r := range []ResidualReason{CrossClient{Clients: []string{"a", "b"}}, EventEntered{Node: "t1"}, LaterOutcome{OutcomeNode: "o", AfterStimulus: "n1a"}} {
		if describe(r) == "" {
			t.Errorf("no description for %T", r)
		}
	}
}

func TestForAcceptance_Golden(t *testing.T) {
	b, err := CanonicalJSON(ForAcceptance(deriveAll(t, "acceptance_crossactor"), acceptanceClients))
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "acceptance_crossactor", b)
}
