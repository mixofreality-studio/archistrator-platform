package scenario

import (
	"fmt"
	"slices"
	"strings"
)

// acceptance.go is the acceptance projection of the derived scenarios (FPC plan 4, Amendment
// C). A client projection (ForComponentAs) keeps the stimuli into one Client with what its
// actor sees; ForAcceptance keeps the whole path as one actor walks it through one Client, and
// AcceptanceResidual names the variants no single client projection covers.

// ForAcceptance is every scenario as one actor walks it through one client: for each scenario
// and each client C ∈ clients (contract keys, compared under Normalize) that some
// actor-originated stimulus targets, the variant "<id>@<C>" keeps, per node, the
// actor-originated stimuli into C (alternatives into other clients at that node dropped),
// every actor-originated stimulus into another client at a node C does not serve, and every
// actor-originated stimulus whose To is not a client (an event entering the system);
// component-to-component stimuli (Input.Kind == "call") are dropped. Seq is renumbered from
// 1; ActorOutcome is kept on every stimulus; ids (apart from the "@<C>" suffix, spelled as
// the client was passed), titles, paths and guards are the source scenario's. A scenario with
// no actor stimulus into a client yields no variant. Variants are sorted by id.
func ForAcceptance(all []Scenario, clients []string) []Scenario {
	ks := clientSpellings(clients)
	var out []Scenario
	for _, v := range variants(all, ks) {
		out = append(out, v.s)
	}
	return out
}

// Residual is one acceptance variant no single client projection covers, and why.
type Residual struct {
	Scenario string // the variant id, "<id>@<client>"
	Reason   ResidualReason
}

// ResidualReason is why an acceptance variant is not covered by one client projection.
//
//sumtype:decl
type ResidualReason interface{ isResidualReason() }

// CrossClient: the variant's actor-originated stimuli target more than one client (some actor
// node has no stimulus into the cover client), so the path leaves the client mid-way.
type CrossClient struct{ Clients []string }

// EventEntered: the variant's first stimulus is a timeEvent or acceptEvent whose To is not a client.
type EventEntered struct{ Node string }

// LaterOutcome: an ActorOutcome output of stimulus AfterStimulus is seen by an actor that did
// not originate it (Output.To ∉ {Input.From} ∪ Input.Via), or the stimulus's To is not a
// client (no client projection keeps its ActorOutcome: stimuliTo zeroes it for a non-client).
type LaterOutcome struct{ OutcomeNode, AfterStimulus string }

func (CrossClient) isResidualReason()  {}
func (EventEntered) isResidualReason() {}
func (LaterOutcome) isResidualReason() {}

// AcceptanceResidual is ForAcceptance(all, clients) filtered to the residual, one entry per
// (variant, reason) in variant then node order; a variant with several reasons has several entries.
func AcceptanceResidual(all []Scenario, clients []string) []Residual {
	ks := clientSpellings(clients)
	var out []Residual
	for _, v := range variants(all, ks) {
		for _, r := range reasons(v.s, ks) {
			out = append(out, Residual{Scenario: v.s.ID, Reason: r})
		}
	}
	return out
}

// IsResidual reports whether variant id names a residual variant (the gate's and testgen's one question).
func IsResidual(rs []Residual, id string) bool {
	return slices.ContainsFunc(rs, func(r Residual) bool { return r.Scenario == id })
}

// describe is the reason as one line of prose for error messages and hold comments.
func describe(r ResidualReason) string {
	switch v := r.(type) {
	case CrossClient:
		return "the path leaves its client mid-way (" + strings.Join(v.Clients, ", ") + ")"
	case EventEntered:
		return "the path is entered by an event at " + v.Node + ", before any client"
	case LaterOutcome:
		return fmt.Sprintf("outcome %s is seen by an actor other than the one who made stimulus %s", v.OutcomeNode, v.AfterStimulus)
	}
	return ""
}

// clientSet maps each client's Normalize key to its spelling as passed (the
// lexicographically smallest spelling when several fold to one key).
type clientSet map[string]string

func clientSpellings(clients []string) clientSet {
	sorted := slices.Clone(clients)
	slices.Sort(sorted)
	ks := clientSet{}
	for _, c := range sorted {
		if _, ok := ks[Normalize(c)]; !ok {
			ks[Normalize(c)] = c
		}
	}
	return ks
}

func (ks clientSet) has(key string) bool { _, ok := ks[key]; return ok }

// variant is one acceptance variant and the client that covers it.
type variant struct {
	s     Scenario
	cover string // the cover client's Normalize key
}

// variants is every scenario's variants, one per cover client, sorted by variant id.
func variants(all []Scenario, ks clientSet) []variant {
	var out []variant
	for _, s := range all {
		for _, key := range sortedKeys(ks) {
			if v, ok := variantFor(s, key, ks); ok {
				out = append(out, v)
			}
		}
	}
	slices.SortStableFunc(out, func(a, b variant) int { return strings.Compare(a.s.ID, b.s.ID) })
	return out
}

func sortedKeys(ks clientSet) []string {
	keys := make([]string, 0, len(ks))
	for k := range ks {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// variantFor is s walked through client key; false when no actor stimulus targets it.
func variantFor(s Scenario, key string, ks clientSet) (variant, bool) {
	served := servedNodes(s, key)
	if len(served) == 0 {
		return variant{}, false
	}
	cp := s
	cp.ID = s.ID + "@" + ks[key]
	cp.Stimuli = nil
	for _, st := range s.Stimuli {
		if !keptInVariant(st, key, served, ks) {
			continue
		}
		st.Seq = len(cp.Stimuli) + 1
		cp.Stimuli = append(cp.Stimuli, st)
	}
	return variant{s: cp, cover: key}, true
}

// servedNodes are the nodes where some actor stimulus of s lands on client key.
func servedNodes(s Scenario, key string) map[string]bool {
	served := map[string]bool{}
	for _, st := range s.Stimuli {
		if st.Input.Kind != "call" && st.Input.To == key {
			served[st.Node] = true
		}
	}
	return served
}

// keptInVariant: an actor stimulus is kept unless it is an alternative into another client at a
// node the cover client serves.
func keptInVariant(st Stimulus, key string, served map[string]bool, ks clientSet) bool {
	if st.Input.Kind == "call" {
		return false
	}
	return !served[st.Node] || st.Input.To == key || !ks.has(st.Input.To)
}

// reasons is why variant v is residual: CrossClient, EventEntered, then each LaterOutcome in
// stimulus then output order.
func reasons(v Scenario, ks clientSet) []ResidualReason {
	var out []ResidualReason
	if r, ok := crossClient(v, ks); ok {
		out = append(out, r)
	}
	if r, ok := eventEntered(v, ks); ok {
		out = append(out, r)
	}
	for _, r := range laterOutcomes(v, ks) {
		out = append(out, r)
	}
	return out
}

// crossClient: the distinct clients the variant's stimuli target, in path order, when several.
func crossClient(v Scenario, ks clientSet) (CrossClient, bool) {
	var targeted []string
	for _, st := range v.Stimuli {
		if ks.has(st.Input.To) && !slices.Contains(targeted, ks[st.Input.To]) {
			targeted = append(targeted, ks[st.Input.To])
		}
	}
	return CrossClient{Clients: targeted}, len(targeted) > 1
}

// eventEntered: the first stimulus is an event into a non-client.
func eventEntered(v Scenario, ks clientSet) (EventEntered, bool) {
	if len(v.Stimuli) == 0 {
		return EventEntered{}, false
	}
	first := v.Stimuli[0]
	isEvent := first.Input.Kind == "timeEvent" || first.Input.Kind == "acceptEvent"
	return EventEntered{Node: first.Node}, isEvent && !ks.has(first.Input.To)
}

// laterOutcomes: the actor-visible outputs no client projection asserts for the actor who sees them.
func laterOutcomes(v Scenario, ks clientSet) []LaterOutcome {
	var out []LaterOutcome
	for _, st := range v.Stimuli {
		for _, o := range st.ActorOutcome.ExpectedOutputs {
			if ks.has(st.Input.To) && originatedBy(st.Input, o.To) {
				continue
			}
			r := LaterOutcome{OutcomeNode: o.Node, AfterStimulus: st.Node}
			if !slices.Contains(out, r) {
				out = append(out, r)
			}
		}
	}
	return out
}

// originatedBy reports whether actor made the stimulus: its caller, or one of the
// alternative callers that collapsed into it.
func originatedBy(in InputEv, actor string) bool {
	key := Normalize(actor)
	return Normalize(in.From) == key || slices.ContainsFunc(in.Via, func(a string) bool { return Normalize(a) == key })
}
