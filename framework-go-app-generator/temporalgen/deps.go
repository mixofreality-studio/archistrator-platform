package temporalgen

import (
	"path"
	"sort"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// Framework import paths threaded into every emitted activities/invokers file.
const (
	fwManagerPath = "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwRAPath      = "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// raDep is one ResourceAccess component dependency of the manager, resolved to
// everything the activities/invokers emitters need: the struct field /
// method-name stem, the interface type, the RA package alias + import path,
// and its ops sorted by name.
type raDep struct {
	field      string // PascalCase dep name: struct field + method-name prefix
	iface      string // RA interface type name (e.g. "Access")
	alias      string // RA package alias (goPackage's last segment)
	importPath string // full RA package import path
	component  string // dep.Component — the serviceContracts key, for ActivityName
	depName    string // dep.Name — the CallerKeyedOps lookup key
	ops        []projectmodel.Operation
}

// resolveRADeps resolves the manager's ResourceAccess component deps in
// contract order. A dep is an RA component dep iff it names a component whose
// contract layer is ResourceAccess; Engine deps and plain deps are skipped.
func resolveRADeps(ec emitContext) []raDep {
	var out []raDep
	for _, dep := range ec.mgr.Deps {
		if dep.Component == "" {
			continue
		}
		c, ok := ec.model.Contracts[dep.Component]
		if !ok || c.Layer != "ResourceAccess" {
			continue
		}
		ops := append([]projectmodel.Operation(nil), c.Doc.Interface.Operations...)
		sort.Slice(ops, func(i, j int) bool { return ops[i].Name < ops[j].Name })
		out = append(out, raDep{
			field:      upperFirst(dep.Name),
			iface:      c.Doc.Interface.Name,
			alias:      path.Base(c.GoPackage),
			importPath: ec.cfg.ModulePath + "/" + c.GoPackage,
			component:  dep.Component,
			depName:    dep.Name,
			ops:        ops,
		})
	}
	return out
}

// resolveGoType maps an RA op param/result schema node to the Go type the
// generated code must use. It is the cross-package counterpart to
// projectmodel.GoType (which the http/mcp generators consume with
// manager-package-local semantics and must keep unchanged).
//
// The one behavioural difference: a schema node that binds a BARE (package-dot
// free) x-go-type with no x-go-import — an exported type hand-written IN the RA
// package itself, not part of its schema-first contract (e.g.
// projectstate.Project / OperatingModel / ArtifactModel) — is emitted qualified
// with the RA package alias. projectmodel.GoType leaves such a name bare, which
// resolves only inside the RA package (where modelgen emits contract.gen.go);
// here the code lands in the CONSUMING manager package, where the bare name is
// undefined. Dotted x-go-types (fwra.IdempotencyKey, uuid.UUID) and $ref types
// are delegated to projectmodel.GoType unchanged.
func resolveGoType(n *projectmodel.SchemaNode, ptr bool, alias string) string {
	if alias != "" && isBareRAType(n) {
		t := alias + "." + n.XGoType
		if ptr {
			return "*" + t
		}
		return t
	}
	return projectmodel.GoType(n, ptr, alias)
}

// isBareRAType reports whether a schema node binds an unqualified, exported Go
// type hand-written in the RA package: a non-empty x-go-type with no package
// dot, no x-go-import, whose first rune is an uppercase ASCII letter. The
// uppercase-first check excludes the predeclared builtins (int, string, bool,
// float64) and builtin composites (e.g. []byte) that also carry bare
// x-go-types in data-contract $defs but must never be package-qualified.
func isBareRAType(n *projectmodel.SchemaNode) bool {
	if n == nil || n.XGoType == "" || n.XGoImport != "" {
		return false
	}
	if strings.Contains(n.XGoType, ".") {
		return false
	}
	r := n.XGoType[0]
	return r >= 'A' && r <= 'Z'
}

// isCallerKeyed reports whether op opName on dep depName takes an explicit
// caller-supplied idempotency key.
func isCallerKeyed(ec emitContext, depName, opName string) bool {
	for _, o := range ec.cfg.CallerKeyedOps[depName] {
		if o == opName {
			return true
		}
	}
	return false
}

// fwraIdempotencyKeyType is the canonical Go type an RA contract op binds
// (via x-go-type) on the param it dedups on — the RA's own enforcement slot.
const fwraIdempotencyKeyType = "fwra.IdempotencyKey"

// isKeyParam reports whether an op param is an explicit idempotency-key param:
// one whose contract schema binds the foundational Go type fwra.IdempotencyKey
// (encoded x-go-type "fwra.IdempotencyKey", exactly as the RA contract emits
// it — e.g. archistrator's operatedSystemStateAccess ops). The enforcing RA
// dedups on THIS param, not on fwra.Context.IdempotencyKey, so the emitter
// must fill it with the derived (or caller-supplied) key and hide it from the
// generated activity/invoker signatures — it must never be workflow-supplied.
// x-go-type is the robust discriminator (the same foundational-type binding
// modelgen uses); fwra is always imported in the emitted files, so no extra
// import wiring is needed.
func isKeyParam(p projectmodel.Param) bool {
	return p.Schema != nil && p.Schema.XGoType == fwraIdempotencyKeyType
}

// businessParams returns the op params that appear in the generated
// activity/invoker signatures: every param except explicit key params (those
// are filled by the emitter, not by the caller).
func businessParams(op projectmodel.Operation) []projectmodel.Param {
	out := make([]projectmodel.Param, 0, len(op.Params))
	for _, p := range op.Params {
		if !isKeyParam(p) {
			out = append(out, p)
		}
	}
	return out
}

// joinImportGroups renders a full "import (...)" block from ordered groups
// (e.g. stdlib, sdk, framework, module), skipping empty groups and separating
// the rest with a blank line. gofmt sorts within each group.
func joinImportGroups(groups [][]string) string {
	var nonEmpty []string
	for _, g := range groups {
		if len(g) > 0 {
			nonEmpty = append(nonEmpty, "\t"+strings.Join(g, "\n\t"))
		}
	}
	return "import (\n" + strings.Join(nonEmpty, "\n\n") + "\n)\n\n"
}

// foundationalImports collects the deduplicated x-go-import paths bound by the
// ops' params and results (uuid.UUID, time.Time, ...), in first-seen order.
func foundationalImports(deps []raDep) []string {
	seen := map[string]bool{}
	var out []string
	for _, rd := range deps {
		for _, op := range rd.ops {
			out = collectOpImports(op, seen, out)
		}
	}
	return out
}

// collectOpImports appends the foundational import paths bound by one op's
// params and result to out, deduplicating via seen.
func collectOpImports(op projectmodel.Operation, seen map[string]bool, out []string) []string {
	for _, p := range op.Params {
		out = appendGoImports(p.Schema, seen, out)
	}
	return appendGoImports(op.Result, seen, out)
}

// appendGoImports appends a schema node's x-go-import paths to out, skipping any
// already present in seen.
func appendGoImports(n *projectmodel.SchemaNode, seen map[string]bool, out []string) []string {
	for _, imp := range projectmodel.GoImports(n) {
		if !seen[imp] {
			seen[imp] = true
			out = append(out, imp)
		}
	}
	return out
}

// importLine quotes an import path as an import-spec line.
func importLine(p string) string { return `"` + p + `"` }

// isStdlib reports whether an import path is a standard-library package (its
// first path segment carries no dot, i.e. no domain).
func isStdlib(p string) bool {
	seg := p
	if i := strings.Index(p, "/"); i >= 0 {
		seg = p[:i]
	}
	return !strings.Contains(seg, ".")
}

// upperFirst uppercases the first rune (e.g. "orderState" -> "OrderState").
func upperFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] -= 'a' - 'A'
	}
	return string(r)
}
