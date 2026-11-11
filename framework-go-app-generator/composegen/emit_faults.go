package composegen

import (
	"fmt"
	"go/format"
	"strconv"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// FaultsFile is the generated fault wrapper of a container that serves web-exposed Managers, beside
// null.gen.go. A web scenario's failNext (Plan 3 Task 3.5) is registered on the scenario host
// serving the arrange endpoint; this wrapper asks that host, before each Manager operation, whether
// the operation was told to fail, and answers it with the Manager error kind of the registered code.
const FaultsFile = "faults.gen.go"

// emitFaults renders FaultsFile for r's web-exposed Managers: a fault wrapper per Manager and
// applyFaults, which RunGenerated calls once, right before applyNullOps, so the null wrapper stays
// outermost.
func emitFaults(m *projectmodel.Model, r *resolved) ([]byte, error) {
	g := &scenarioGen{m: m, r: r, imp: newImportSet(), byKey: map[string]scenarioComp{}}
	for _, mc := range r.webMgrs {
		c := m.Contracts[mc.key]
		if c == nil || c.Doc == nil {
			return nil, fmt.Errorf("composegen: fault wrapper: manager %q has no contract document to wrap", mc.key)
		}
		g.imp.add(mc.importPath, importAliasFor(mc.importPath, mc.alias))
		comp := scenarioComp{key: mc.key, iface: mc.iface, alias: mc.alias, importPath: mc.importPath, contract: c}
		if err := g.emitFault(comp); err != nil {
			return nil, err
		}
	}
	for _, p := range []string{"encoding/json", "fmt", "net/http", "net/url", "time"} {
		g.imp.add(p, "")
	}
	g.imp.add(pathFwManager, "fwm")
	var b strings.Builder
	b.WriteString(genHeader)
	b.WriteString("\npackage " + r.pkgName + "\n\n")
	b.WriteString("// The fault wrapper of web scenarios (Plan 3 Task 3.5): with a scenario host's address configured,\n")
	b.WriteString("// the web-exposed Managers fail the operations a scenario registered with failNext, over every\n")
	b.WriteString("// transport at once. A test instrument: the local profile only.\n\n")
	b.WriteString(g.imp.render())
	b.WriteString(faultsSource)
	b.WriteString(g.body.String())
	b.WriteString(applyFaultsSource(r))
	src, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, wrapFormatErr(err, b.String())
	}
	return src, nil
}

// emitFault renders the fault<Iface> wrapper of one Manager: every operation that can return an
// error asks the host first; the rest pass through.
func (g *scenarioGen) emitFault(comp scenarioComp) error {
	iface, typ := comp.iface, comp.alias+"."+comp.iface
	ctxType := "fwm.Context"
	fmt.Fprintf(&g.body, "// fault%s wraps a %s, failing the operations the scenario host was told to.\n", iface, iface)
	fmt.Fprintf(&g.body, "type fault%s struct {\n\tinner %s\n\ttake  func(component, op string) (string, bool)\n}\n\n", iface, typ)
	for _, op := range comp.contract.Doc.Interface.Operations {
		if err := g.emitFaultMethod(comp, op, ctxType); err != nil {
			return err
		}
	}
	return nil
}

// emitFaultMethod renders one wrapper method.
func (g *scenarioGen) emitFaultMethod(comp scenarioComp, op projectmodel.Operation, ctxType string) error {
	params := []string{"rc " + ctxType}
	args := []string{"rc"}
	for i, p := range op.Params {
		if err := g.checkExported(comp, op, p.Schema); err != nil {
			return err
		}
		name := "p" + strconv.Itoa(i)
		params = append(params, name+" "+scenarioGoType(p.Schema, p.Pointer, comp.alias))
		args = append(args, name)
		g.addSchemaImports(p.Schema)
	}
	if err := g.checkExported(comp, op, op.Result); err != nil {
		return err
	}
	g.addSchemaImports(op.Result)
	ret, _ := nullResults(op, comp.alias)
	call := "f.inner." + op.Name + "(" + strings.Join(args, ", ") + ")"
	fmt.Fprintf(&g.body, "func (f fault%s) %s(%s)%s {\n", comp.iface, op.Name, strings.Join(params, ", "), ret)
	switch {
	case op.Error && op.Result != nil:
		t := scenarioGoType(op.Result, false, comp.alias)
		fmt.Fprintf(&g.body, "\tif code, ok := f.take(%q, %q); ok {\n\t\tvar zero %s\n\t\treturn zero, faultError(code)\n\t}\n", comp.key, op.Name, t)
		fmt.Fprintf(&g.body, "\treturn %s\n}\n\n", call)
	case op.Error:
		fmt.Fprintf(&g.body, "\tif code, ok := f.take(%q, %q); ok {\n\t\treturn faultError(code)\n\t}\n", comp.key, op.Name)
		fmt.Fprintf(&g.body, "\treturn %s\n}\n\n", call)
	case ret == "":
		fmt.Fprintf(&g.body, "\t%s\n}\n\n", call)
	default:
		fmt.Fprintf(&g.body, "\treturn %s\n}\n\n", call)
	}
	return nil
}

// applyFaultsSource renders applyFaults(profile, url, managers): the bundle unchanged when no scenario
// host address is configured; an error under any profile but local; otherwise each Manager in its fault
// wrapper.
func applyFaultsSource(r *resolved) string {
	var b strings.Builder
	b.WriteString("// applyFaults wraps each web-exposed Manager in its fault wrapper when url, the scenario host's\n")
	b.WriteString("// address, is set. Unset, it returns managers unchanged under every profile; set under any profile\n")
	b.WriteString("// but " + localProfile + ", it refuses, so a deployed binary cannot be told to take faults from a test host.\n")
	b.WriteString("func applyFaults(profile, url string, managers WebManagers) (WebManagers, error) {\n")
	b.WriteString("\tif url == \"\" {\n\t\treturn managers, nil\n\t}\n")
	fmt.Fprintf(&b, "\tif profile != %q {\n", localProfile)
	fmt.Fprintf(&b, "\t\treturn managers, fmt.Errorf(\"a scenario host address is set under profile %%q: fault injection is a %s-profile test instrument, so the process refuses to start\", profile)\n\t}\n", localProfile)
	b.WriteString("\ttake := faultTaker(url)\n")
	for _, mc := range r.webMgrs {
		fmt.Fprintf(&b, "\tmanagers.%s = fault%s{inner: managers.%s, take: take}\n", mc.iface, mc.iface, mc.iface)
	}
	b.WriteString("\treturn managers, nil\n}\n")
	return b.String()
}

const faultsSource = `// faultTimeout bounds one question to the scenario host.
const faultTimeout = 2 * time.Second

// faultTaker asks the scenario host at url whether the next call of (component, op) was told to
// fail: GET <url>/faults/take answers 200 {"errorCode"} for a registered fault (consumed by the
// asking) and 204 for none. A host that cannot be asked fails the call (code "scenariohost
// unreachable") instead of letting a scenario run on as if its fault had not been registered.
func faultTaker(base string) func(component, op string) (string, bool) {
	client := &http.Client{Timeout: faultTimeout}
	return func(component, op string) (string, bool) {
		q := url.Values{"component": {component}, "op": {op}}
		res, err := client.Get(base + "/faults/take?" + q.Encode())
		if err != nil {
			return "scenariohost unreachable: " + err.Error(), true
		}
		defer func() { _ = res.Body.Close() }()
		switch res.StatusCode {
		case http.StatusNoContent:
			return "", false
		case http.StatusOK:
			var body struct {
				ErrorCode string ` + "`json:\"errorCode\"`" + `
			}
			if err := json.NewDecoder(res.Body).Decode(&body); err != nil || body.ErrorCode == "" {
				return "scenariohost answered a fault with no error code", true
			}
			return body.ErrorCode, true
		}
		return fmt.Sprintf("scenariohost answered %d", res.StatusCode), true
	}
}

// faultKinds is the Manager error kind of each error code a plan may name: the kind's own name or the
// wire code a generated handler answers it with. A code of no kind is an infrastructure failure.
var faultKinds = map[string]fwm.Kind{
	"ContractMisuse":     fwm.ContractMisuse,
	"contract_misuse":    fwm.ContractMisuse,
	"NotFound":           fwm.NotFound,
	"not_found":          fwm.NotFound,
	"Unauthorized":       fwm.Unauthorized,
	"forbidden":          fwm.Unauthorized,
	"FailedPrecondition": fwm.FailedPrecondition,
	"failed_precondition": fwm.FailedPrecondition,
	"Infrastructure":     fwm.Infrastructure,
	"infrastructure":     fwm.Infrastructure,
}

// faultError is the error an operation told to fail with code answers with; its detail names the
// code, so the refusal a client sees says the failure was arranged.
func faultError(code string) error {
	kind, ok := faultKinds[code]
	if !ok {
		kind = fwm.Infrastructure
	}
	return fwm.New(kind, "scenario fault: "+code)
}

`
