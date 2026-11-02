package temporalgen

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// invokerStructDoc is the doc comment on the generated genInvokers struct.
const invokerStructDoc = "// genInvokers is the workflow-side typed call surface: one method per\n" +
	"// generated activity. Workflows hold one; no string activity names and no\n" +
	"// interface{} results appear in hand-written workflow code. Opts, when\n" +
	"// non-nil, overrides the per-op ActivityOptions (the manager's hand-written\n" +
	"// preset hook); the generated default applies otherwise.\n"

// invokerStruct is the genInvokers struct emitted verbatim: a single Opts
// override hook, no per-dep fields (unlike genActivities, invokers carry no
// state — every call is routed through workflow.ExecuteActivity by name).
const invokerStruct = invokerStructDoc +
	"type genInvokers struct {\n" +
	"\tOpts func(activityName string) (workflow.ActivityOptions, bool)\n" +
	"}\n\n"

// invokerOptionsHelper is the generated ActivityOptions preset + the
// options(ctx, name) lookup helper, emitted verbatim.
const invokerOptionsHelper = `// genDefaultActivityOptions is the uniform generated preset: 15s
// StartToClose, terminal on ContractMisuse (the workflow-level Conflict
// re-read loop and category presets live in the manager's Opts hook).
func genDefaultActivityOptions() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: 15 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{fwmanager.RAErrType(fwra.ContractMisuse)},
		},
	}
}

func (i genInvokers) options(ctx workflow.Context, name string) workflow.Context {
	opts := genDefaultActivityOptions()
	if i.Opts != nil {
		if o, ok := i.Opts(name); ok {
			opts = o
		}
	}
	return workflow.WithActivityOptions(ctx, opts)
}

`

// emitInvokers generates invokers.gen.go: the workflow-side typed call
// surface, one method per generated activity (one per RA dep operation),
// with the same name and business params as the wrapped activity (ctx
// workflow.Context in place of context.Context).
func emitInvokers(ec emitContext) ([]byte, error) {
	deps := resolveRADeps(ec)

	var methods strings.Builder
	for _, rd := range deps {
		for _, op := range rd.ops {
			methods.WriteString(invokerMethod(ec, rd, op))
		}
	}
	used, err := qualifiersUsed(methods.String())
	if err != nil {
		return nil, err
	}

	var b strings.Builder
	b.WriteString(genHeader)
	b.WriteString("\npackage " + ec.pkgName + "\n\n")
	b.WriteString(invokerImports(deps, used))
	b.WriteString(invokerStruct)
	b.WriteString(invokerOptionsHelper)
	b.WriteString(methods.String())

	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, err
	}
	return formatted, nil
}

// qualifiersUsed is the package qualifiers the invoker methods select from. An invoker names
// a dep's package only through a contract type, so a dep whose operations take and return
// only inline types must not be imported at all.
func qualifiersUsed(methods string) (map[string]bool, error) {
	f, err := parser.ParseFile(token.NewFileSet(), "", "package p\n"+methods, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	used := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok {
				used[id.Name] = true
			}
		}
		return true
	})
	return used, nil
}

// invokerImports builds the grouped import block: stdlib ("time"), the
// Temporal SDK (temporal, workflow), the framework packages, then the RA dep
// packages the methods name (+ any foundational x-go-import types the ops
// reference). gofmt sorts within each group.
func invokerImports(deps []raDep, used map[string]bool) string {
	stdlib := []string{importLine("time")}
	sdk := []string{
		importLine("go.temporal.io/sdk/temporal"),
		importLine("go.temporal.io/sdk/workflow"),
	}
	framework := []string{
		"fwmanager " + importLine(fwManagerPath),
		"fwra " + importLine(fwRAPath),
	}
	var module []string
	for _, rd := range deps {
		if used[rd.alias] {
			module = append(module, importLine(rd.importPath))
		}
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

// invokerMethod emits one invoker method: same name and business params as
// the activity it wraps (activityMethod), calling workflow.ExecuteActivity by
// the registered name instead.
func invokerMethod(ec emitContext, rd raDep, op projectmodel.Operation) string {
	name := rd.field + op.Name
	registered := ActivityName(rd.component, op.Name)
	keyed := isCallerKeyed(ec, rd.depName, op.Name)

	var b strings.Builder
	fmt.Fprintf(&b, "// %s invokes activity %q.\n", name, registered)
	b.WriteString("func (i genInvokers) " + name +
		"(" + invokerParams(rd, op, keyed) + ") " + activityResults(rd, op) + " {\n")
	b.WriteString(invokerBody(rd, op, keyed, registered))
	b.WriteString("}\n\n")
	return b.String()
}

// invokerParams builds the invoker method parameter list: ctx
// workflow.Context first, an explicit caller key second for CallerKeyedOps,
// then the business params with the RA-alias-qualified Go types — identical
// to the wrapped activity's param list except for the ctx type. Explicit RA
// key params are omitted (the activity fills them), so the workflow can never
// supply the RA's dedup key.
func invokerParams(rd raDep, op projectmodel.Operation, keyed bool) string {
	parts := []string{"ctx workflow.Context"}
	if keyed {
		parts = append(parts, "key fwra.IdempotencyKey")
	}
	for _, p := range businessParams(op) {
		parts = append(parts, p.Name+" "+resolveGoType(p.Schema, p.Pointer, rd.alias))
	}
	return strings.Join(parts, ", ")
}

// invokerBody emits the invoker method body: workflow.ExecuteActivity with
// the registered name (the options lookup and the activity name both use
// it), the same args as the wrapped activity (the caller key first for
// CallerKeyedOps, then the business params), decoded into a typed var — or
// discarded via Get(ctx, nil) for result-less ops.
func invokerBody(rd raDep, op projectmodel.Operation, keyed bool, registered string) string {
	args := []string{fmt.Sprintf("i.options(ctx, %q)", registered), fmt.Sprintf("%q", registered)}
	if keyed {
		args = append(args, "key")
	}
	// Explicit RA key params are not forwarded — the activity fills them from
	// the derived/caller key, matching the activity's param list exactly.
	for _, p := range businessParams(op) {
		args = append(args, p.Name)
	}
	call := "workflow.ExecuteActivity(" + strings.Join(args, ", ") + ")"
	if op.Result == nil {
		return "\terr := " + call + ".Get(ctx, nil)\n\treturn err\n"
	}
	typ := resolveGoType(op.Result, false, rd.alias)
	return "\tvar out " + typ + "\n\terr := " + call + ".Get(ctx, &out)\n\treturn out, err\n"
}
