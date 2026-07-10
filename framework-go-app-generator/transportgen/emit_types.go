package transportgen

import (
	"bytes"
	"fmt"
	"go/format"
	"sort"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/modelgen"
)

// emitSharedTypes emits the byte-identical DTOs shared across managers ONCE, in
// types_shared.gen.go.
func emitSharedTypes(an *defAnalysis, cfg Config) ([]byte, error) {
	defs := make([]namedDef, 0, len(an.sharedOrder))
	for _, name := range an.sharedOrder {
		defs = append(defs, namedDef{name, an.sharedBody[name]})
	}
	return emitTypeFile(defs, cfg)
}

// emitManagerTypes emits a manager's own DTOs (unique defs bare, conflict defs
// prefixed) in types_<mgr>.gen.go. Shared defs are skipped — they live in
// types_shared.gen.go, in the same flat package.
func emitManagerTypes(info *mgrInfo, an *defAnalysis, cfg Config) ([]byte, error) {
	names := make([]string, 0, len(info.doc.Defs))
	for name := range info.doc.Defs {
		names = append(names, name)
	}
	sort.Strings(names)

	defs := make([]namedDef, 0, len(names))
	for _, name := range names {
		body := info.doc.Defs[name]
		switch {
		case an.shared[name]:
			continue
		case an.conflict[name]:
			nb, err := transformConflictBody(body, info.base, info.rename)
			if err != nil {
				return nil, fmt.Errorf("transform %s: %w", name, err)
			}
			defs = append(defs, namedDef{info.rename[name], nb})
		default: // unique
			defs = append(defs, namedDef{name, rewriteRefs(body, info.rename)})
		}
	}
	if len(defs) == 0 {
		// A manager whose every DTO is shared still needs a (near-empty) file only
		// if it declares defs; emit just the header/package so the file set is
		// uniform. In practice every manager owns at least its result DTOs.
		return formatFile(genHeader + "package " + cfg.PackageName + "\n")
	}
	return emitTypeFile(defs, cfg)
}

// emitTypeFile runs modelgen.EmitTypes over the ordered defs, re-headers the
// output with the transportgen marker, and appends a varname string bridge
// (<Type>Name) for every enum.
func emitTypeFile(defs []namedDef, cfg Config) ([]byte, error) {
	src, err := modelgen.EmitTypes(buildDefsDoc(defs), modelgen.TypeOptions{
		PackageName:  cfg.PackageName,
		UUIDAsString: cfg.UUIDAsString,
	})
	if err != nil {
		return nil, err
	}
	body := stripModelgenHeader(src)
	bridges := bridgesFor(defs)
	assembled := genHeader + body
	if bridges != "" {
		assembled += "\n" + bridges
	}
	return formatFile(assembled)
}

// stripModelgenHeader drops EmitTypes' modelgen file header, returning the source
// from its "package" clause onward.
func stripModelgenHeader(src []byte) string {
	s := string(src)
	if i := strings.Index(s, "\npackage "); i >= 0 {
		return s[i+1:]
	}
	return s
}

// bridgesFor emits, for every enum among defs, a varname string bridge
//
//	func <Type>Name(v <Type>) string { switch v { case <Varname>: return "<Varname>" ... } }
//
// mapping an enum value to its declared varname — the reusable replacement for
// the hand transports' ordinal tables. Non-enum defs contribute nothing.
func bridgesFor(defs []namedDef) string {
	var b strings.Builder
	for _, d := range defs {
		vns, isEnum := enumVarnames(d.body)
		if !isEnum {
			continue
		}
		fmt.Fprintf(&b, "// %sName returns the declared varname of a %s value.\n", d.name, d.name)
		fmt.Fprintf(&b, "func %sName(v %s) string {\n\tswitch v {\n", d.name, d.name)
		for _, vn := range vns {
			fmt.Fprintf(&b, "\tcase %s:\n\t\treturn %q\n", vn, vn)
		}
		b.WriteString("\tdefault:\n\t\treturn \"\"\n\t}\n}\n\n")
	}
	return b.String()
}

// emitExternalTypes emits opaque json.RawMessage aliases for the dangling
// in-package types a contract binds via x-go-type but never declares.
func emitExternalTypes(names []string, cfg Config) ([]byte, error) {
	var b strings.Builder
	b.WriteString(genHeader)
	fmt.Fprintf(&b, "package %s\n\n", cfg.PackageName)
	b.WriteString("import \"encoding/json\"\n\n")
	b.WriteString("// These aliases stand in for server in-package types a contract binds via\n")
	b.WriteString("// x-go-type but does not carry in its $defs; the SDK keeps them opaque so a\n")
	b.WriteString("// caller can still round-trip the enclosing DTO.\n")
	for _, n := range names {
		fmt.Fprintf(&b, "type %s = json.RawMessage\n", n)
	}
	return formatFile(b.String())
}

// formatFile gofmt's an assembled source file.
func formatFile(src string) ([]byte, error) {
	out, err := format.Source([]byte(src))
	if err != nil {
		return nil, fmt.Errorf("gofmt generated source: %w\n%s", err, src)
	}
	return out, nil
}

// writeImports emits an import block for the given stdlib paths (sorted).
func writeImports(b *bytes.Buffer, paths []string) {
	if len(paths) == 0 {
		return
	}
	sorted := append([]string(nil), paths...)
	sort.Strings(sorted)
	b.WriteString("import (\n")
	for _, p := range sorted {
		fmt.Fprintf(b, "\t%q\n", p)
	}
	b.WriteString(")\n\n")
}
