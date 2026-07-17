package modelgen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// GenerateFakes emits a Fake<Iface> test double per built service-contract
// interface, into that interface's contract package's SIBLING <base>fake
// package (e.g. contract package "internal/resourceaccess/orderstate",
// package orderstate -> fake package "internal/resourceaccess/orderstate/fake",
// package orderstatefake). It walks the SAME parse -> sort -> group pipeline
// Generate uses (parseServiceContracts, groupByGoPackage), and every emitted
// method's signature is built from the identical layerContext/paramType/
// returnClause mechanics emitInterface itself uses (see emit_fake.go's
// emitFake) — so a Fake<Iface> satisfies the real generated interface,
// asserted at the bottom of each entry's block via
// `var _ <base>.<Iface> = (*Fake<Iface>)(nil)`.
//
// The result is keyed by "<goPackage>/fake" (the sibling package's own
// directory, e.g. "internal/resourceaccess/orderstate/fake") — a caller
// writes the bytes to filepath.Join(key, "fake.gen.go"). A goPackage whose
// every entry is interface-less (a stub, or a contract with no `interface`
// key) is skipped entirely — no key is emitted for it, mirroring Generate's
// skip of a goPackage with no built entries.
//
// GenerateFakes is purely additive: Generate's own output is byte-for-byte
// unaffected (fakeQualifyAlias, the one shared toggle it uses, is reset to ""
// before returning from every pass — see emitFakeGoPackage — and Generate/
// EmitTypes never set it).
func GenerateFakes(projectJSON []byte, cfg Config) (map[string][]byte, error) {
	if cfg.ModulePath == "" {
		return nil, fmt.Errorf("modelgen: Config.ModulePath is empty")
	}
	contracts, err := parseServiceContracts(projectJSON)
	if err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(contracts))
	for k := range contracts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	groupOrder, groups, err := groupByGoPackage(keys, contracts)
	if err != nil {
		return nil, err
	}

	out := map[string][]byte{}
	for _, goPkg := range groupOrder {
		src, wrote, err := emitFakeGoPackage(goPkg, groups[goPkg], contracts, cfg.ModulePath)
		if err != nil {
			return nil, err
		}
		if !wrote {
			continue
		}
		out[goPkg+"/fake"] = src
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no built service contracts with an interface found (nothing to fake)")
	}
	return out, nil
}

// emitFakeGoPackage emits fake.gen.go for one goPackage group: a Fake<Iface>
// per contract key in the group that carries an interface, in ascending key
// order (keys is already sorted — the same deterministic order Generate's
// genGroup uses). wrote=false (no error) means no key in the group had an
// interface to fake — the caller skips the goPackage entirely.
func emitFakeGoPackage(goPkg string, keys []string, contracts map[string]json.RawMessage, modulePath string) ([]byte, bool, error) {
	alias := filepath.Base(goPkg)
	pendingImports = map[string]string{}
	fakeQualifyAlias = alias
	defer func() { fakeQualifyAlias = "" }()

	var buf bytes.Buffer
	wrote := false
	for _, k := range keys {
		iface, haveIface, err := parseInterfaceOnly(contracts[k])
		if err != nil {
			return nil, false, fmt.Errorf("modelgen fake %q: %w", k, err)
		}
		if !haveIface {
			continue
		}
		if err := validateExportedRefs(k, iface); err != nil {
			return nil, false, err
		}
		emitFake(&buf, iface)
		wrote = true
	}
	if !wrote {
		return nil, false, nil
	}
	// The compile-time assertion (and any $ref/bare-exported param or result
	// type) references the contract package via this bare selector — register
	// its import now that we know the group produced at least one fake.
	pendingImports[modulePath+"/"+goPkg] = ""

	src, err := formatGeneratedFakeFile(alias+"fake", &buf)
	if err != nil {
		return nil, false, fmt.Errorf("modelgen fake %q: %w", goPkg, err)
	}
	return src, true, nil
}

// parseInterfaceOnly decodes one contract entry's `interface` key only — all
// GenerateFakes needs, since a fake never re-declares the contract's $defs
// types, only references them via the contract package selector (unlike
// Generate/genGroup, which also need $defs for the types block).
func parseInterfaceOnly(raw json.RawMessage) (Interface, bool, error) {
	var doc jsonschema.Schema
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Interface{}, false, fmt.Errorf("parse schema: %w", err)
	}
	return decodeInterface(&doc)
}

// emitFake writes one contract's Fake<Iface> test double: a struct with one
// <Op>Fn func field per operation (iface.Operations order — the authoritative
// order, matching emitInterface), a delegating method per op that panics if
// its Fn is unset, and the compile-time interface assertion. Every signature
// is built from ifaceSignature/returnClause — the identical
// layerContext/paramType/returnClause mechanics emitInterface itself uses —
// so Fake<Iface> structurally satisfies <alias>.<Iface> (fakeQualifyAlias
// must already be set to alias by the caller; see emitFakeGoPackage).
func emitFake(buf *bytes.Buffer, iface Interface) {
	lc, hasLayer := layerContext[iface.Layer]
	struct_ := "Fake" + iface.Name
	alias := fakeQualifyAlias

	fmt.Fprintf(buf, "// %s is a generated test double for %s.%s: set the Fn field(s)\n", struct_, alias, iface.Name)
	fmt.Fprintf(buf, "// a test needs; calling a method whose Fn is unset panics.\n")
	fmt.Fprintf(buf, "type %s struct {\n", struct_)
	for _, op := range iface.Operations {
		fmt.Fprintf(buf, "\t%sFn func(%s)%s\n", op.Name, ifaceSignature(lc, hasLayer, op), returnClause(op))
	}
	buf.WriteString("}\n\n")

	for _, op := range iface.Operations {
		emitFakeMethod(buf, struct_, lc, hasLayer, op)
	}

	fmt.Fprintf(buf, "var _ %s.%s = (*%s)(nil)\n\n", alias, iface.Name, struct_)
}

// emitFakeMethod writes one delegating method: the signature (identical to
// the field's Fn signature), a panic guard when the Fn is unset, and the call
// through to it — `return f.<Op>Fn(args...)` for the three shapes with a
// return value, or a bare call with no `return` for the no-return shape (see
// returnClause).
func emitFakeMethod(buf *bytes.Buffer, struct_ string, lc struct{ alias, path, typ string }, hasLayer bool, op Operation) {
	sig := ifaceSignature(lc, hasLayer, op)
	args := ifaceArgs(hasLayer, op)
	fmt.Fprintf(buf, "func (f *%s) %s(%s)%s {\n", struct_, op.Name, sig, returnClause(op))
	fmt.Fprintf(buf, "\tif f.%sFn == nil {\n\t\tpanic(%q)\n\t}\n", op.Name, struct_+"."+op.Name+"Fn not set")
	if op.Result != nil || op.Error {
		fmt.Fprintf(buf, "\treturn f.%sFn(%s)\n", op.Name, args)
	} else {
		fmt.Fprintf(buf, "\tf.%sFn(%s)\n", op.Name, args)
	}
	buf.WriteString("}\n\n")
}

// ifaceSignature renders one op's named parameter list — the layer context
// (named "rc") first when the layer defines one, then each param's name +
// paramType — the identical mechanics emitInterface uses for its own methods,
// so a Fake<Iface> method's parameter list matches the real interface
// method's (modulo the contract-package qualification paramType/goType apply
// while fakeQualifyAlias is set).
func ifaceSignature(lc struct{ alias, path, typ string }, hasLayer bool, op Operation) string {
	params := make([]string, 0, len(op.Params)+1)
	if hasLayer {
		params = append(params, "rc "+lc.typ)
		pendingImports[lc.path] = lc.alias
	}
	for _, p := range op.Params {
		params = append(params, p.Name+" "+paramType(p))
	}
	return strings.Join(params, ", ")
}

// ifaceArgs renders the bare argument names a delegating call passes through
// (the layer context + each param, no types), in the same order
// ifaceSignature renders them.
func ifaceArgs(hasLayer bool, op Operation) string {
	args := make([]string, 0, len(op.Params)+1)
	if hasLayer {
		args = append(args, "rc")
	}
	for _, p := range op.Params {
		args = append(args, p.Name)
	}
	return strings.Join(args, ", ")
}

// validateExportedRefs errors if any $ref type name reachable from iface's
// operations (params or result) is unexported: emitType emits a $def's `type
// <name> ...` verbatim (no forced export), so a lowercase name is a valid
// SAME-package type in the contract package but cannot be named from the
// sibling <base>fake package GenerateFakes emits into. Detected explicitly
// here so GenerateFakes never emits code that fails to compile.
func validateExportedRefs(key string, iface Interface) error {
	for _, op := range iface.Operations {
		for _, p := range op.Params {
			if name, bad := firstUnexportedRef(p.Schema); bad {
				return fmt.Errorf("modelgen fake %q: %s.%s param %q references unexported type %q; the fake package cannot import it — export %s or keep %s's fake in the same package", key, iface.Name, op.Name, p.Name, name, name, iface.Name)
			}
		}
		if name, bad := firstUnexportedRef(op.Result); bad {
			return fmt.Errorf("modelgen fake %q: %s.%s result references unexported type %q; the fake package cannot import it — export %s or keep %s's fake in the same package", key, iface.Name, op.Name, name, name, iface.Name)
		}
	}
	return nil
}

// firstUnexportedRef walks s the same way goType does ($ref, an array's
// items, an open object's additionalProperties) looking for a $ref whose $def
// name is unexported. x-go-type nodes are never flagged here: a
// dotted/imported binding is already fully qualified, and a bare exported one
// is handled by goTypeForXGoType's fake-qualification path — only a $ref to a
// lowercase $def name is unrenderable from a sibling package.
func firstUnexportedRef(s *jsonschema.Schema) (string, bool) {
	if s == nil {
		return "", false
	}
	if s.Ref != "" {
		return unexportedRefName(s.Ref)
	}
	return firstUnexportedRefInContainer(s)
}

// unexportedRefName reports the $def name a "#/$defs/Name" ref resolves to,
// and whether that name is unexported.
func unexportedRefName(ref string) (string, bool) {
	name := refName(ref)
	if name != "" && !isExportedIdent(name) {
		return name, true
	}
	return "", false
}

// firstUnexportedRefInContainer handles the two container shapes goType
// recurses into: an array's Items, and an open object's AdditionalProperties
// (goType never looks at an object's named Properties — see goTypeForType).
func firstUnexportedRefInContainer(s *jsonschema.Schema) (string, bool) {
	switch effectiveType(s) {
	case "array":
		return firstUnexportedRef(s.Items)
	case "object":
		if len(s.Properties) == 0 && s.AdditionalProperties != nil {
			return firstUnexportedRef(s.AdditionalProperties)
		}
	}
	return "", false
}
