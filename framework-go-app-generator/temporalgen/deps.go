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
	iface      string // RA interface type name (e.g. "OrderStateAccess")
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
