package scenario

import (
	"fmt"
	"go/token"
	"slices"
	"strings"
)

// clientops.go is the grammar of a call label's op clause and what the framework derives from
// it. The op clause is the label's text before any "—" or "→"; the rest is prose. Inside the
// clause "|" separates alternative operations, and nowhere else: "RecognizeActor(proof) →
// member | refused" lists outcomes.

// opClause is the label's text before its first "—" or "→" at bracket depth zero.
func opClause(label string) string {
	depth := 0
	for i, r := range label {
		switch r {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case '—', '→':
			if depth == 0 {
				return label[:i]
			}
		}
	}
	return label
}

// clauseAlternatives splits an op clause on "|" at bracket depth zero.
func clauseAlternatives(clause string) []string {
	var out []string
	var cur strings.Builder
	depth := 0
	for _, r := range clause {
		switch r {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case '|':
			if depth == 0 {
				out = append(out, cur.String())
				cur.Reset()
				continue
			}
		}
		cur.WriteRune(r)
	}
	return append(out, cur.String())
}

// LeadingOpNames is the label's op clause as the exported operation names it leads with, one
// per "|" alternative in label order. An alternative that is anything else, such as prose
// ("requests one list action"), or an unexported name, comes back in invalid.
func LeadingOpNames(label string) (ops, invalid []string) {
	for _, alt := range clauseAlternatives(opClause(label)) {
		alt = strings.TrimSpace(alt)
		name := leadingIdent(alt)
		rest := strings.TrimPrefix(alt, name)
		if token.IsIdentifier(name) && token.IsExported(name) && (rest == "" || strings.HasPrefix(rest, "(")) {
			ops = append(ops, name)
			continue
		}
		invalid = append(invalid, alt)
	}
	return ops, invalid
}

// ClientOps derives a browser surface's operations from the labels of the actor calls into it:
// the distinct exported names they lead with, spelled literally, in first-seen order. Design
// owns the names, so nothing here slugs prose or capitalises. Two spellings that fold to the
// same name are refused, because a label resolves its op ignoring case and could not tell them
// apart.
func ClientOps(labels []string) ([]string, error) {
	var names []string
	for _, label := range labels {
		leading, invalid := LeadingOpNames(label)
		if len(invalid) > 0 {
			return nil, fmt.Errorf("the label %q must lead with exported operation names, such as \"AddTodo | RewordTodo — performs one list action\", and %q is not one", label, invalid[0])
		}
		names = append(names, leading...)
	}
	return distinctOps(names)
}

// distinctOps keeps each name once, in first-seen order, and refuses two spellings of one.
func distinctOps(names []string) ([]string, error) {
	var ops []string
	seen := map[string]string{}
	for _, name := range names {
		key := Normalize(name)
		other, dup := seen[key]
		if dup && other != name {
			return nil, fmt.Errorf("the labels spell one operation two ways, %q and %q; use one spelling", other, name)
		}
		if !dup {
			seen[key] = name
			ops = append(ops, name)
		}
	}
	return ops, nil
}

// UnresolvedAlternatives is the "|" alternatives of a label's op clause that name no operation
// of the callee, once the callee has a contract. A label with no "|" is not its business: a
// prose label into a contracted component is legal and carries no op. Without this, an
// alternative that fails to resolve would silently lose its stimulus.
func UnresolvedAlternatives(in Input, component, label string) []string {
	alts := clauseAlternatives(opClause(label))
	if len(alts) < 2 {
		return nil
	}
	fam := family(in, component)
	if len(fam) == 0 {
		return nil
	}
	byKey := opsByKey(fam)
	var out []string
	for _, alt := range alts {
		if _, ok := byKey[Normalize(leadingIdent(alt))]; !ok {
			out = append(out, strings.TrimSpace(alt))
		}
	}
	return out
}

// FamilyOps is the sorted operation names of every contract a call into component resolves
// against, and whether it has any such contract.
func FamilyOps(in Input, component string) ([]string, bool) {
	fam := family(in, component)
	if len(fam) == 0 {
		return nil, false
	}
	var out []string
	for _, kc := range fam {
		for _, op := range kc.contract.Ops {
			out = append(out, op.Name)
		}
	}
	slices.Sort(out)
	return out, true
}

// ClientOpName is the operation a machine client exposes for a Manager's: the Manager's interface
// name without its stereotype, then the operation, as its generated tool's Go method is spelled.
func ClientOpName(managerInterface, op string) string {
	return strings.TrimSuffix(managerInterface, "Manager") + op
}

// throughManagerOps resolves a call into a machine client whose label is prose, such as "submits a
// title", through what the client itself does in the same step: "addTodo(proof, title)" into the
// Manager names the operation the actor reached. The design draws the actor's request and the
// client's call side by side, and a machine client adds nothing between them.
func throughManagerOps(in Input, client string, step []Call) []opTarget {
	if !isClientContract(in, client) {
		return nil
	}
	byKey := opsByKey(family(in, client))
	var out []opTarget
	seen := map[opTarget]bool{}
	for _, c := range ClientManagerOps(in, client, step) {
		ot, ok := byKey[Normalize(c.Name())]
		if ok && !seen[ot] {
			seen[ot] = true
			out = append(out, ot)
		}
	}
	return out
}

func isClientContract(in Input, component string) bool {
	fam := family(in, component)
	return len(fam) > 0 && fam[0].contract.Client
}

// ManagerOp is one operation a client's call in a step resolves on the contract behind it.
type ManagerOp struct {
	Contract  string // the callee's Contracts key
	Interface string // the callee's interface name
	Op        string // the callee's operation
}

// Name is the operation the client exposes for it.
func (m ManagerOp) Name() string { return ClientOpName(m.Interface, m.Op) }

// ClientManagerOps is what a step's calls from client into a contracted component resolve, in call
// order: the design's whole statement of what is reached through that client.
func ClientManagerOps(in Input, client string, step []Call) []ManagerOp {
	want := Normalize(client)
	var out []ManagerOp
	for _, c := range step {
		if Normalize(c.From) != want {
			continue
		}
		for _, ot := range resolveOps(in, c.To, c.Label) {
			key, _ := contractKey(in, ot.to)
			out = append(out, ManagerOp{Contract: key, Interface: in.Contracts[key].Interface, Op: ot.op})
		}
	}
	return out
}

// ReachesClient reports whether a step has a call into client, which is what makes the client's own
// calls in that step operations someone reaches through it.
func ReachesClient(client string, step []Call) bool {
	want := Normalize(client)
	return slices.ContainsFunc(step, func(c Call) bool { return Normalize(c.To) == want })
}
