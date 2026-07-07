package temporalgen

import (
	"fmt"
	"go/format"
	"path"
	"sort"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// Framework import paths threaded into every emitted activities file.
const (
	fwManagerPath = "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwRAPath      = "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// activityStructDoc is the doc comment on the generated genActivities struct.
const activityStructDoc = "// genActivities hosts one Temporal Activity per operation of each\n" +
	"// ResourceAccess component dependency — the manager's architecture-approved\n" +
	"// call surface. Fields are the contract interfaces, threaded by RegisterWorker.\n"

// activityKeyHelper is the run-scoped idempotency-key deriver emitted verbatim.
const activityKeyHelper = `// genActivityIdempotencyKey derives the run-scoped 3-part key
// workflowID:runID:activityID (stable per logical write within a run,
// distinct across runs of the same workflow ID).
func genActivityIdempotencyKey(ctx context.Context) fwra.IdempotencyKey {
	info := activity.GetInfo(ctx)
	return fwra.IdempotencyKey(fmt.Sprintf("%s:%s:%s",
		info.WorkflowExecution.ID, info.WorkflowExecution.RunID, info.ActivityID))
}

`

// raDep is one ResourceAccess component dependency of the manager, resolved to
// everything the activities emitter needs: the struct field / method-name stem,
// the interface type, the RA package alias + import path, and its ops sorted by
// name.
type raDep struct {
	field      string // PascalCase dep name: struct field + method-name prefix
	iface      string // RA interface type name (e.g. "OrderStateAccess")
	alias      string // RA package alias (goPackage's last segment)
	importPath string // full RA package import path
	component  string // dep.Component — the serviceContracts key, for ActivityName
	depName    string // dep.Name — the CallerKeyedOps lookup key
	ops        []projectmodel.Operation
}

// emitActivities generates activities.gen.go: one Temporal Activity per
// operation of each ResourceAccess component dependency of the manager, in
// contract (dep) order, ops sorted by name.
func emitActivities(ec emitContext) ([]byte, error) {
	deps := activityRADeps(ec)

	var b strings.Builder
	b.WriteString(genHeader)
	b.WriteString("\npackage " + ec.pkgName + "\n\n")
	b.WriteString(activityImports(deps))
	b.WriteString(activityStruct(deps))
	b.WriteString(activityKeyHelper)
	for _, rd := range deps {
		for _, op := range rd.ops {
			b.WriteString(activityMethod(ec, rd, op))
		}
	}

	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, err
	}
	return formatted, nil
}

// activityRADeps resolves the manager's ResourceAccess component deps in
// contract order. A dep is an RA component dep iff it names a component whose
// contract layer is ResourceAccess; Engine deps and plain deps are skipped.
func activityRADeps(ec emitContext) []raDep {
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

// activityImports builds the grouped import block: stdlib, the Temporal SDK,
// the framework packages, then the RA dep packages (+ any foundational
// x-go-import types the ops reference). gofmt sorts within each group.
func activityImports(deps []raDep) string {
	stdlib := []string{importLine("context"), importLine("fmt")}
	sdk := []string{importLine("go.temporal.io/sdk/activity")}
	framework := []string{
		"fwmanager " + importLine(fwManagerPath),
		"fwra " + importLine(fwRAPath),
	}
	var module []string
	for _, rd := range deps {
		module = append(module, importLine(rd.importPath))
	}
	for _, imp := range foundationalImports(deps) {
		if isStdlib(imp) {
			stdlib = append(stdlib, importLine(imp))
		} else {
			module = append(module, importLine(imp))
		}
	}

	var groups []string
	for _, g := range [][]string{stdlib, sdk, framework, module} {
		if len(g) > 0 {
			groups = append(groups, "\t"+strings.Join(g, "\n\t"))
		}
	}
	return "import (\n" + strings.Join(groups, "\n\n") + "\n)\n\n"
}

// activityStruct emits the genActivities struct: one interface-typed field per
// RA dep, in dep order.
func activityStruct(deps []raDep) string {
	var b strings.Builder
	b.WriteString(activityStructDoc)
	b.WriteString("type genActivities struct {\n")
	for _, rd := range deps {
		b.WriteString("\t" + rd.field + " " + rd.alias + "." + rd.iface + "\n")
	}
	b.WriteString("}\n\n")
	return b.String()
}

// activityMethod emits one activity method wrapping a single RA op.
func activityMethod(ec emitContext, rd raDep, op projectmodel.Operation) string {
	name := rd.field + op.Name
	registered := ActivityName(rd.component, op.Name)
	keyed := isCallerKeyed(ec, rd.depName, op.Name)

	var b strings.Builder
	fmt.Fprintf(&b, "// %s wraps %s.\n", name, registered)
	fmt.Fprintf(&b, "// Registered as %q.\n", registered)
	b.WriteString("func (a *genActivities) " + name +
		"(" + activityParams(rd, op, keyed) + ") " + activityResults(rd, op) + " {\n")
	b.WriteString(activityBody(rd, op, keyed))
	b.WriteString("}\n\n")
	return b.String()
}

// activityParams builds the method parameter list: always ctx first, an
// explicit caller key second for CallerKeyedOps, then the business params with
// the RA-alias-qualified Go types.
func activityParams(rd raDep, op projectmodel.Operation, keyed bool) string {
	parts := []string{"ctx context.Context"}
	if keyed {
		parts = append(parts, "key fwra.IdempotencyKey")
	}
	for _, p := range op.Params {
		parts = append(parts, p.Name+" "+projectmodel.GoType(p.Schema, p.Pointer, rd.alias))
	}
	return strings.Join(parts, ", ")
}

// activityResults returns the method result list: "(T, error)" for ops with a
// result, "error" for ops without.
func activityResults(rd raDep, op projectmodel.Operation) string {
	if op.Result == nil {
		return "error"
	}
	return "(" + projectmodel.GoType(op.Result, false, rd.alias) + ", error)"
}

// activityBody emits the method body: build the fwra.Context (derived key, or
// the caller key for CallerKeyedOps), call the RA op, thread fwmanager.MapError.
func activityBody(rd raDep, op projectmodel.Operation, keyed bool) string {
	keyExpr := "genActivityIdempotencyKey(ctx)"
	if keyed {
		keyExpr = "key"
	}
	args := []string{"fwra.Context{Context: ctx, IdempotencyKey: " + keyExpr + "}"}
	for _, p := range op.Params {
		args = append(args, p.Name)
	}
	call := "a." + rd.field + "." + op.Name + "(" + strings.Join(args, ", ") + ")"
	if op.Result == nil {
		return "\terr := " + call + "\n\treturn fwmanager.MapError(err)\n"
	}
	return "\tv, err := " + call + "\n\treturn v, fwmanager.MapError(err)\n"
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
