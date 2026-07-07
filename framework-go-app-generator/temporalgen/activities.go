package temporalgen

import (
	"fmt"
	"go/format"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
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

// emitActivities generates activities.gen.go: one Temporal Activity per
// operation of each ResourceAccess component dependency of the manager, in
// contract (dep) order, ops sorted by name.
func emitActivities(ec emitContext) ([]byte, error) {
	deps := resolveRADeps(ec)

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
	return joinImportGroups([][]string{stdlib, sdk, framework, module})
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
// the RA-alias-qualified Go types. Explicit RA key params (fwra.IdempotencyKey)
// are omitted — the activity body fills them (see activityBody), never the
// workflow.
func activityParams(rd raDep, op projectmodel.Operation, keyed bool) string {
	parts := []string{"ctx context.Context"}
	if keyed {
		parts = append(parts, "key fwra.IdempotencyKey")
	}
	for _, p := range businessParams(op) {
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
// Explicit RA key params are filled IN PLACE with the same key expression that
// feeds the context field, preserving the contract's param order — the derived
// key (or the caller key) reaches the RA's dedup slot, never a workflow value.
func activityBody(rd raDep, op projectmodel.Operation, keyed bool) string {
	keyExpr := "genActivityIdempotencyKey(ctx)"
	if keyed {
		keyExpr = "key"
	}
	args := []string{"fwra.Context{Context: ctx, IdempotencyKey: " + keyExpr + "}"}
	for _, p := range op.Params {
		if isKeyParam(p) {
			args = append(args, keyExpr)
			continue
		}
		args = append(args, p.Name)
	}
	call := "a." + rd.field + "." + op.Name + "(" + strings.Join(args, ", ") + ")"
	if op.Result == nil {
		return "\terr := " + call + "\n\treturn fwmanager.MapError(err)\n"
	}
	return "\tv, err := " + call + "\n\treturn v, fwmanager.MapError(err)\n"
}
