package temporalgen

import (
	"fmt"
	"go/format"
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

	var b strings.Builder
	b.WriteString(genHeader)
	b.WriteString("\npackage " + ec.pkgName + "\n\n")
	b.WriteString(invokerImports(deps))
	b.WriteString(invokerStruct)
	b.WriteString(invokerOptionsHelper)
	for _, rd := range deps {
		for _, op := range rd.ops {
			b.WriteString(invokerMethod(ec, rd, op))
		}
	}

	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, err
	}
	return formatted, nil
}

// invokerImports builds the grouped import block: stdlib ("time"), the
// Temporal SDK (temporal, workflow), the framework packages, then the RA dep
// packages (+ any foundational x-go-import types the ops reference). gofmt
// sorts within each group.
func invokerImports(deps []raDep) string {
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
// to the wrapped activity's param list except for the ctx type.
func invokerParams(rd raDep, op projectmodel.Operation, keyed bool) string {
	parts := []string{"ctx workflow.Context"}
	if keyed {
		parts = append(parts, "key fwra.IdempotencyKey")
	}
	for _, p := range op.Params {
		parts = append(parts, p.Name+" "+projectmodel.GoType(p.Schema, p.Pointer, rd.alias))
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
	for _, p := range op.Params {
		args = append(args, p.Name)
	}
	call := "workflow.ExecuteActivity(" + strings.Join(args, ", ") + ")"
	if op.Result == nil {
		return "\terr := " + call + ".Get(ctx, nil)\n\treturn err\n"
	}
	typ := projectmodel.GoType(op.Result, false, rd.alias)
	return "\tvar out " + typ + "\n\terr := " + call + ".Get(ctx, &out)\n\treturn out, err\n"
}
