package transportgen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/modelgen"
)

// namedDef is one $def as it will be fed to modelgen.EmitTypes: a (possibly
// prefixed) name and its (possibly ref-rewritten) raw JSON body.
type namedDef struct {
	name string
	body json.RawMessage
}

// defAnalysis is the global cross-manager DTO partition.
type defAnalysis struct {
	// bodyByMgr[mgr][name] is a manager's raw $def body.
	bodyByMgr map[string]map[string]json.RawMessage
	// conflict is the set of names emitted per-manager (prefixed): either their
	// emitted Go differs across declaring managers, or they transitively
	// reference such a name.
	conflict map[string]bool
	// shared is the set of names emitted ONCE (byte-identical Go, referencing
	// only other shared names).
	shared map[string]bool
	// sharedOrder is shared, sorted, for deterministic emission.
	sharedOrder []string
	// sharedBody[name] is the canonical raw body for a shared def (any declaring
	// manager's — they emit identically).
	sharedBody map[string]json.RawMessage
}

// analyzeDefs computes the shared/conflict partition. A def is a conflict when
// (a) it is declared by >1 manager and its ISOLATED emitted Go differs across
// them, or (b) it references (transitively) a conflict def — a byte-identical
// wrapper whose meaning still diverges because a referent diverges (e.g.
// ReviewFeedback is textually identical everywhere but references AnchoredComment,
// which is not). Single-manager (unique) defs are neither shared nor conflict:
// they keep their bare name and only rewrite references to conflict names.
func analyzeDefs(infos []*mgrInfo, cfg Config) (*defAnalysis, error) {
	an := &defAnalysis{
		bodyByMgr:  map[string]map[string]json.RawMessage{},
		conflict:   map[string]bool{},
		shared:     map[string]bool{},
		sharedBody: map[string]json.RawMessage{},
	}
	declarers := indexDeclarers(infos, an)
	candidateShared, err := seedPartition(declarers, an, cfg)
	if err != nil {
		return nil, err
	}
	for promoteRound(candidateShared, declarers, an) {
	}
	for name := range candidateShared {
		an.shared[name] = true
		an.sharedOrder = append(an.sharedOrder, name)
	}
	sort.Strings(an.sharedOrder)
	return an, nil
}

// indexDeclarers records each manager's raw $def bodies on an and returns a map
// of def name -> the managers declaring it (cfg order preserved).
func indexDeclarers(infos []*mgrInfo, an *defAnalysis) map[string][]string {
	declarers := map[string][]string{}
	for _, info := range infos {
		an.bodyByMgr[info.key] = info.doc.Defs
		names := make([]string, 0, len(info.doc.Defs))
		for name := range info.doc.Defs {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			declarers[name] = append(declarers[name], info.key)
		}
	}
	return declarers
}

// seedPartition classifies every multi-manager def as a shared candidate (its
// isolated Go agrees across declarers) or a conflict (it diverges).
func seedPartition(declarers map[string][]string, an *defAnalysis, cfg Config) (map[string]bool, error) {
	candidateShared := map[string]bool{}
	for name, mgrs := range declarers {
		if len(mgrs) < 2 {
			continue // unique
		}
		same, canonical, err := isolatedAgree(name, mgrs, an.bodyByMgr, cfg)
		if err != nil {
			return nil, err
		}
		if same {
			candidateShared[name] = true
			an.sharedBody[name] = canonical
		} else {
			an.conflict[name] = true
		}
	}
	return candidateShared, nil
}

// promoteRound demotes every shared candidate that references a conflict name to
// a conflict, returning whether anything changed (the fixpoint driver).
func promoteRound(candidateShared map[string]bool, declarers map[string][]string, an *defAnalysis) bool {
	changed := false
	for name := range candidateShared {
		if an.conflict[name] || !candidateRefsConflict(name, declarers[name], an) {
			continue
		}
		an.conflict[name] = true
		delete(candidateShared, name)
		delete(an.sharedBody, name)
		changed = true
	}
	return changed
}

// isolatedAgree reports whether name emits byte-identical Go across every
// manager that declares it, returning a canonical body (the first manager's in
// cfg order) when so. Emission is per-def context-free (a $def's Go depends only
// on its own body — $refs resolve by name string), so an isolated single-def
// EmitTypes is a faithful equality probe.
func isolatedAgree(name string, mgrs []string, bodyByMgr map[string]map[string]json.RawMessage, cfg Config) (bool, json.RawMessage, error) {
	var first []byte
	var canonical json.RawMessage
	for _, mgr := range mgrs {
		body := bodyByMgr[mgr][name]
		src, err := modelgen.EmitTypes(buildDefsDoc([]namedDef{{name, body}}), modelgen.TypeOptions{
			PackageName:  cfg.PackageName,
			UUIDAsString: cfg.UUIDAsString,
		})
		if err != nil {
			return false, nil, fmt.Errorf("isolate %s in %s: %w", name, mgr, err)
		}
		if first == nil {
			first = src
			canonical = body
			continue
		}
		if !bytes.Equal(first, src) {
			return false, nil, nil
		}
	}
	return true, canonical, nil
}

// candidateRefsConflict reports whether name, in any declaring manager, has a
// $ref to a currently-conflict name.
func candidateRefsConflict(name string, mgrs []string, an *defAnalysis) bool {
	for _, mgr := range mgrs {
		for _, ref := range extractRefs(an.bodyByMgr[mgr][name]) {
			if an.conflict[ref] {
				return true
			}
		}
	}
	return false
}

var refRe = regexp.MustCompile(`"#/\$defs/([A-Za-z0-9_]+)"`)

// extractRefs returns the $def names a raw body references.
func extractRefs(body json.RawMessage) []string {
	var out []string
	for _, m := range refRe.FindAllStringSubmatch(string(body), -1) {
		out = append(out, m[1])
	}
	return out
}

// buildDefsDoc assembles a minimal contract document {"$defs":{...}} from the
// ordered defs, writing each body VERBATIM so struct property order (which
// modelgen recovers from the raw JSON) survives untouched.
func buildDefsDoc(defs []namedDef) json.RawMessage {
	var b bytes.Buffer
	b.WriteString(`{"$defs":{`)
	for i, d := range defs {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Quote(d.name))
		b.WriteByte(':')
		b.Write(d.body)
	}
	b.WriteString(`}}`)
	return b.Bytes()
}

// rewriteRefs rewrites every "#/$defs/<old>" to its renamed target, longest name
// first so no name is a prefix-substring hazard. Preserves all other bytes
// (property order intact).
func rewriteRefs(body json.RawMessage, rename map[string]string) json.RawMessage {
	if len(rename) == 0 {
		return body
	}
	names := make([]string, 0, len(rename))
	for n := range rename {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })
	s := string(body)
	for _, n := range names {
		s = replaceAll(s, `"#/$defs/`+n+`"`, `"#/$defs/`+rename[n]+`"`)
	}
	return json.RawMessage(s)
}

func replaceAll(s, old, new string) string {
	return string(bytes.ReplaceAll([]byte(s), []byte(old), []byte(new)))
}

// transformConflictBody prepares a conflict def's body for its per-manager file:
// references to sibling conflict names are rewritten, and — for an enum — its
// x-enum-varnames are prefixed so the generated const names never collide with a
// same-named sibling enum's consts in the flat package.
func transformConflictBody(body json.RawMessage, prefix string, rename map[string]string) (json.RawMessage, error) {
	body = rewriteRefs(body, rename)
	if _, isEnum := enumVarnames(body); !isEnum {
		return body, nil
	}
	// Enums carry no object properties, so a map round-trip is order-safe.
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	var vns []string
	if err := json.Unmarshal(m["x-enum-varnames"], &vns); err != nil {
		return nil, err
	}
	for i := range vns {
		vns[i] = prefix + vns[i]
	}
	nb, err := json.Marshal(vns)
	if err != nil {
		return nil, err
	}
	m["x-enum-varnames"] = nb
	return json.Marshal(m)
}

// collectDangling finds bare (import-less, exported-identifier) x-go-type
// bindings that reference NO contract $def — server in-package types a contract
// leans on (e.g. StaleCauseView) that the self-contained SDK does not carry. The
// SDK stands these in as opaque json.RawMessage aliases so its DTOs still
// compile. Builtins ([]byte) and imported foundationals (uuid.UUID/time.Time/
// json.RawMessage) are excluded by isBareTypeName / the x-go-import check.
func collectDangling(infos []*mgrInfo) []string {
	known := map[string]bool{}
	for _, info := range infos {
		for name := range info.doc.Defs {
			known[name] = true
		}
	}
	set := map[string]bool{}
	for _, info := range infos {
		for _, body := range info.doc.Defs {
			var v any
			if json.Unmarshal(body, &v) == nil {
				walkDangling(v, known, set)
			}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func walkDangling(v any, known, set map[string]bool) {
	switch t := v.(type) {
	case map[string]any:
		noteDangling(t, known, set)
		for _, vv := range t {
			walkDangling(vv, known, set)
		}
	case []any:
		for _, vv := range t {
			walkDangling(vv, known, set)
		}
	}
}

// noteDangling records t's x-go-type when it is an import-less, undeclared
// in-package type reference.
func noteDangling(t map[string]any, known, set map[string]bool) {
	xt, ok := t["x-go-type"].(string)
	if !ok {
		return
	}
	if _, hasImport := t["x-go-import"]; hasImport {
		return
	}
	if isBareTypeName(xt) && !known[xt] {
		set[xt] = true
	}
}

// isBareTypeName reports whether s is a plain exported Go identifier (an
// in-package type reference) — not a builtin like []byte, not a qualified
// foundational like uuid.UUID.
func isBareTypeName(s string) bool {
	if s == "" || s[0] < 'A' || s[0] > 'Z' {
		return false
	}
	for _, r := range s {
		if !isIdentRune(r) {
			return false
		}
	}
	return true
}

// isIdentRune reports whether r may appear in a Go identifier body.
func isIdentRune(r rune) bool {
	return r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// enumVarnames returns a def's x-enum-varnames in order, and whether the def is
// an enum with a bound varname list.
func enumVarnames(body json.RawMessage) ([]string, bool) {
	var probe struct {
		VarNames []string `json:"x-enum-varnames"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return nil, false
	}
	if len(probe.VarNames) == 0 {
		return nil, false
	}
	return probe.VarNames, true
}
