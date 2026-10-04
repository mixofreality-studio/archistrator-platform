package testgen

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Hook naming (deterministic-component-testing design §12 B3). The hooks file
// is agent-owned and linted like any other source file, and B3 forbids both
// //nolint and a lint-config exemption, so every symbol testgen spells there
// is one revive's var-naming rule accepts. One grammar names them and the
// SAME grammar classifies them in the drift check:
//
//	newSubject[<Iface>]
//	step[<Iface>]<ScenarioID>S<seq>
//	probe[<Iface>]<ScenarioID>S<seq>N<n>
//	replayWorkflows (a manager package only; one per package — §12 B4)
//
// e.g. stepUC3P2S1 / probeUC3P2S1N2. The step's input type the hook takes is
// input[<Iface>]<ScenarioID>S<seq> (declared in the generated file; not a
// hook). The "S"/"N" separators keep the scenario id's own trailing digits
// apart from the step and probe ordinals now that no underscore can.

// Hook symbol prefixes: the subject constructor, a hooked step, a probe on
// another component. The input type prefix names a generated declaration.
const (
	subjectPrefix = "newSubject"
	stepPrefix    = "step"
	probePrefix   = "probe"
	inputPrefix   = "input"
)

// replayHook is the one hook a manager package declares once, whatever its
// contracts: what its workers register, handed to the scenario host's replay
// gate by the generated TestMain (scenariohost.MainWithWorkflows).
const replayHook = "replayWorkflows"

// hookName is the one spelling of every hook symbol: prefix, the contract's
// interface when several contracts share the package, the scenario id, and
// the step / probe ordinals. A zero seq names the subject constructor's shape
// (no scenario); a zero n names a step.
func hookName(prefix, iface, id string, seq, n int) string {
	parts := []string{prefix}
	if iface != "" {
		parts = append(parts, ident(iface))
	}
	if id != "" {
		parts = append(parts, ident(id))
	}
	if seq > 0 {
		parts = append(parts, "S"+strconv.Itoa(seq))
	}
	if n > 0 {
		parts = append(parts, "N"+strconv.Itoa(n))
	}
	return goName(parts...)
}

// hookPattern is the grammar hookName spells, per hook family: after the
// prefix, either nothing (the single-contract subject) or a word that starts
// upper-case or with a digit (goName capitalizes every word after the first),
// then the family's ordinal suffix. Underscores survive goName only between
// two digits.
var hookPattern = regexp.MustCompile(`^(?:` +
	subjectPrefix + `(?:[A-Z0-9][A-Za-z0-9_]*)?` +
	`|` + stepPrefix + `[A-Z0-9][A-Za-z0-9_]*S[0-9]+` +
	`|` + probePrefix + `[A-Z0-9][A-Za-z0-9_]*S[0-9]+N[0-9]+` +
	`|` + replayHook +
	`)$`)

// isHookName reports whether name is spelled like a hook symbol. The drift
// check classifies both the generated file's references and the hooks file's
// declarations with it; an agent helper not spelled like a hook is ignored.
func isHookName(name string) bool { return hookPattern.MatchString(name) }

// goName joins parts with underscores and folds the result to the name
// revive's var-naming rule would suggest, repeating until the rule suggests
// nothing more. One pass is not always enough: "id2" capitalizes to "Id2",
// which the rule then splits at the lower→digit boundary and upper-cases as
// the initialism "ID". Each pass only removes underscores or upper-cases
// letters, so the loop terminates.
func goName(parts ...string) string {
	name := strings.Join(parts, "_")
	for {
		next := lintName(name)
		if next == name {
			return name
		}
		name = next
	}
}

// claim records that owner declares name in this package and refuses a
// second owner: two scenario ids that fold to one Go name (e.g. "a-b" and
// "a_b") would declare one symbol twice, which gofmt accepts but the package
// build does not.
func (g *goEmit) claim(name, owner string) error {
	if prev, ok := g.owners[name]; ok && prev != owner {
		return fmt.Errorf("testgen: %s: %s and %s both fold to the Go name %s", g.goPkg, prev, owner, name)
	}
	g.owners[name] = owner
	return nil
}

// lintName is revive's var-naming suggestion (github.com/mgechev/revive
// v1.15.0, internal/rule/name.go, func Name, MIT licence) with no allow- or
// block-list: split at underscores and lower→non-lower transitions, upper-case
// a word that is a common initialism (lower-case when it leads), capitalize an
// all-lower-case word after the first, and drop underscores except one between
// two digits. A name the rule leaves unchanged passes it.
func lintName(name string) string {
	if name == "_" || isAllLower(name) {
		return name
	}
	runes := []rune(name)
	w, i := 0, 0 // start of the current word, scan position
	for i+1 <= len(runes) {
		var eow bool
		eow, runes = wordEnds(runes, i)
		i++
		if !eow {
			continue
		}
		foldWord(runes, w, i)
		w = i
	}
	return string(runes)
}

func isAllLower(s string) bool {
	for _, r := range s {
		if !unicode.IsLower(r) {
			return false
		}
	}
	return true
}

// wordEnds reports whether a word ends at runes[i], dropping the run of
// underscores after it (all but one when it separates two digits).
func wordEnds(runes []rune, i int) (bool, []rune) {
	switch {
	case i+1 == len(runes):
		return true, runes
	case runes[i+1] == '_':
		n := 1
		for i+n+1 < len(runes) && runes[i+n+1] == '_' {
			n++
		}
		if i+n+1 < len(runes) && unicode.IsDigit(runes[i]) && unicode.IsDigit(runes[i+n+1]) {
			n--
		}
		copy(runes[i+1:], runes[i+n+1:])
		return true, runes[:len(runes)-n]
	default:
		return unicode.IsLower(runes[i]) && !unicode.IsLower(runes[i+1]), runes
	}
}

// foldWord rewrites the word runes[w:i] in place: an initialism upper-cased
// (lower-cased when it leads the name; "IDS" spelled "IDs"), or an all-lower
// word after the first capitalized.
func foldWord(runes []rune, w, i int) {
	word := string(runes[w:i])
	if u := strings.ToUpper(word); commonInitialisms[u] {
		if w == 0 && unicode.IsLower(runes[w]) {
			u = strings.ToLower(u)
		}
		if u == "IDS" {
			u = "IDs"
		}
		copy(runes[w:], []rune(u))
		return
	}
	if w > 0 && strings.ToLower(word) == word {
		runes[w] = unicode.ToUpper(runes[w])
	}
}

// commonInitialisms is revive's list (same source as lintName).
var commonInitialisms = map[string]bool{
	"ACL": true, "API": true, "ASCII": true, "CPU": true, "CSS": true,
	"DNS": true, "EOF": true, "GUID": true, "HTML": true, "HTTP": true,
	"HTTPS": true, "ID": true, "IDS": true, "IP": true, "JSON": true,
	"LHS": true, "QPS": true, "RAM": true, "RHS": true, "RPC": true,
	"SLA": true, "SMTP": true, "SQL": true, "SSH": true, "TCP": true,
	"TLS": true, "TTL": true, "UDP": true, "UI": true, "UID": true,
	"UUID": true, "URI": true, "URL": true, "UTF8": true, "VM": true,
	"XML": true, "XMPP": true, "XSRF": true, "XSS": true,
}
