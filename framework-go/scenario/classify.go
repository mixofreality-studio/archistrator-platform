package scenario

import (
	"strings"
	"unicode"
)

// class is the IOAD classification of one node (spec §2.2).
type class int

const (
	classInternal   class = iota // kept on the path, contributes nothing
	classInput                   // opens a stimulus
	classOutput                  // expected output on the open stimulus
	classState                   // expected state on the open stimulus
	classHint                    // anchored note: hint on the open stimulus
	classStructural              // start, end, merge, fork, join, swimLane, goto, loop, interruptEdge
)

// classify applies spec §2.2 in priority order.
func classify(n Node, in Input, view *View) class {
	switch n.Kind {
	case "acceptEvent", "timeEvent":
		return classInput
	case "sendSignal":
		return classOutput
	case "objectNode":
		return classState
	case "note":
		if n.Anchor != "" {
			return classHint
		}
		return classStructural
	case "start", "end", "merge", "fork", "join", "swimLane", "goto", "loop", "interruptEdge":
		return classStructural
	case "decision", "switch":
		return classifyBranch(n, in)
	case "action":
		return classifyAction(n, in, view)
	}
	return classInternal
}

// classifyBranch: a decision/switch decided by an actor contributes its chosen
// guard as an input (rule 1); decided by a component or unset, it is internal (rule 6).
func classifyBranch(n Node, in Input) class {
	if n.DecidedBy != "" && in.Actors[n.DecidedBy] {
		return classInput
	}
	return classInternal
}

// classifyAction: an action in an actor's lane is an accept event (rule 1); an
// action whose step carries a call to an actor or an external resource is an
// inferred output (rule 2); every other action is internal (rule 6).
func classifyAction(n Node, in Input, view *View) class {
	if n.LinkedActorID != "" && in.Actors[n.LinkedActorID] {
		return classInput
	}
	if callsOutward(in, view, n.ID) {
		return classOutput
	}
	return classInternal
}

// callsOutward reports whether any call of view step nodeID targets an actor,
// or leaves the component graph (neither from an actor nor to a component).
func callsOutward(in Input, view *View, nodeID string) bool {
	for _, c := range stepCalls(view, nodeID) {
		if in.Actors[c.To] || (!in.Actors[c.From] && !isComponent(in, c.To)) {
			return true
		}
	}
	return false
}

// stepCalls returns the calls of every view step attached to nodeID, in step
// then call order; nil when the view is absent. Indices into the result are
// the call indices used for provenance.
func stepCalls(view *View, nodeID string) []Call {
	if view == nil {
		return nil
	}
	var calls []Call
	for _, st := range view.Steps {
		if st.NodeID == nodeID {
			calls = append(calls, st.Calls...)
		}
	}
	return calls
}

// isComponent reports whether id names a contracted component under Normalize.
func isComponent(in Input, id string) bool {
	_, ok := contractFor(in, id)
	return ok
}

// resolveOps is Amendment A2: the label is split on " — ", "→", "/", "·" and
// ";" (outside any bracketed argument list), each piece's leading identifier
// (the text before "(" or whitespace) is compared under Normalize with the
// callee contract's op names, and every piece that matches resolves, in label
// order and without repeats. nil when the callee has no contract or no piece
// names one of its ops.
func resolveOps(in Input, component, label string) []string {
	ct, ok := contractFor(in, component)
	if !ok {
		return nil
	}
	byKey := opsByKey(ct)
	var out []string
	seen := map[string]bool{}
	for _, piece := range labelPieces(label) {
		name, ok := byKey[Normalize(leadingIdent(piece))]
		if !ok || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// opsByKey indexes a contract's op names by Normalize; the first op wins a fold.
func opsByKey(ct Contract) map[string]string {
	byKey := make(map[string]string, len(ct.Ops))
	for _, op := range ct.Ops {
		k := Normalize(op.Name)
		if _, dup := byKey[k]; k != "" && !dup {
			byKey[k] = op.Name
		}
	}
	return byKey
}

// labelPieces splits a call label on the A2 separators at bracket depth zero,
// so a separator inside an argument list never splits it.
func labelPieces(label string) []string {
	var pieces []string
	var cur strings.Builder
	depth := 0
	for _, r := range label {
		switch r {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case '—', '→', '/', '·', ';':
			if depth == 0 {
				pieces = append(pieces, cur.String())
				cur.Reset()
				continue
			}
		}
		cur.WriteRune(r)
	}
	return append(pieces, cur.String())
}

// leadingIdent is the piece's text before its first "(" or whitespace, after
// leading whitespace is trimmed.
func leadingIdent(piece string) string {
	piece = strings.TrimSpace(piece)
	if i := strings.IndexFunc(piece, func(r rune) bool { return r == '(' || unicode.IsSpace(r) }); i >= 0 {
		return piece[:i]
	}
	return piece
}

// contractFor finds the contract keyed by component under Normalize. When
// several keys fold to the same component the lowest key wins, so the result
// never depends on map iteration order.
func contractFor(in Input, component string) (Contract, bool) {
	want := Normalize(component)
	best, found := "", false
	for k := range in.Contracts {
		if Normalize(k) == want && (!found || k < best) {
			best, found = k, true
		}
	}
	if !found {
		return Contract{}, false
	}
	return in.Contracts[best], true
}
