package composegen

import (
	"fmt"
	"go/format"
	"slices"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator/httpgen"
)

// authzRow is one decision a generated transport asks the composition root's policy: the
// Manager operation, its verb, and the owner scope's resource Kind (the ID is always the
// principal's subject). It is read from httpgen.PlanOps, the one derivation the REST handlers
// and the MCP tools both authorize with.
type authzRow struct {
	manager, op, verb, scope string
}

// authzRows is one row per operation each web-exposed Manager's generated transports serve, in
// contract-key then declared-operation order.
func authzRows(r *resolved) ([]authzRow, error) {
	var rows []authzRow
	for _, mc := range r.webMgrs {
		mgrRows, err := managerAuthzRows(mc, r.cfg.ExposedOps)
		if err != nil {
			return nil, err
		}
		rows = append(rows, mgrRows...)
	}
	return rows, nil
}

// clientAuthzRows is the authorization rows of every Manager a machine client's generated handler
// fronts, de-duplicated: the same derivation (httpgen.PlanOps) authz.gen.go reads, reached by way
// of the clients (Task 3.4's generated policy is held to it). It is empty for a model whose clients
// front no built Manager.
func (g *scenarioGen) clientAuthzRows() ([]authzRow, error) {
	seen := map[string]bool{}
	var rows []authzRow
	for _, cl := range g.classifyClients() {
		if seen[cl.manager.key] {
			continue
		}
		seen[cl.manager.key] = true
		mgrRows, err := managerAuthzRows(managerComp{key: cl.manager.key, iface: cl.manager.iface, doc: cl.manager.contract.Doc}, g.r.cfg.ExposedOps)
		if err != nil {
			return nil, err
		}
		rows = append(rows, mgrRows...)
	}
	return rows, nil
}

// managerAuthzRows is one Manager's rows: every operation its transports serve, narrowed by
// exposed as clientgen narrows them (a nil entry or no entry is the whole interface).
func managerAuthzRows(mc managerComp, exposed map[string][]string) ([]authzRow, error) {
	if mc.doc == nil {
		return nil, fmt.Errorf("composegen: manager %q: no contract document to plan its authorization", mc.key)
	}
	plans, err := httpgen.PlanOps(mc.doc)
	if err != nil {
		return nil, fmt.Errorf("composegen: manager %q: %w", mc.key, err)
	}
	served := exposed[mc.key]
	var rows []authzRow
	for _, p := range plans {
		if served == nil || slices.Contains(served, p.Name) {
			rows = append(rows, authzRow{manager: mc.iface, op: p.Name, verb: p.AuthzVerb, scope: p.AuthzScope})
		}
	}
	return rows, nil
}

// emitAuthz renders authz.gen.go (the table) and authz.gen_test.go (the test that holds the
// composition root's policy to it). The test is the framework's, not the app's: it reaches the
// policy through the generated edgeSecurity and the principal through the DevConfig hook, so no
// app writes a probe of its own, and an app's policy that refuses a request its own generated
// transports send goes red in the activity that composes the server.
func emitAuthz(r *resolved) (table, test []byte, err error) {
	rows, err := authzRows(r)
	if err != nil {
		return nil, nil, err
	}
	var b strings.Builder
	b.WriteString(genHeader)
	b.WriteString("\npackage " + r.pkgName + "\n\n")
	b.WriteString(authzTableDoc)
	b.WriteString("var generatedAuthorizations = []generatedAuthorization{\n")
	for _, row := range rows {
		fmt.Fprintf(&b, "\t{Manager: %q, Op: %q, Verb: %q, Scope: %q},\n", row.manager, row.op, row.verb, row.scope)
	}
	b.WriteString("}\n")
	if table, err = format.Source([]byte(b.String())); err != nil {
		return nil, nil, wrapFormatErr(err, b.String())
	}
	src := genHeader + "\npackage " + r.pkgName + "\n" + authzTestBody
	if test, err = format.Source([]byte(src)); err != nil {
		return nil, nil, wrapFormatErr(err, src)
	}
	return table, test, nil
}

const authzTableDoc = `// generatedAuthorization is one decision a generated transport asks the composition root's
// policy: security.Action{Verb: Verb} on security.ResourceRef{Kind: Scope, ID: <the
// principal's subject>}. Every generated request is owner-scoped; ownership of a resource an
// operation names is the Manager's to resolve.
type generatedAuthorization struct {
	Manager string // the Manager interface the operation belongs to
	Op      string // the operation
	Verb    string // the security.Action.Verb asked
	Scope   string // the security.ResourceRef.Kind asked: the principal's own <manager>Catalog
}

// generatedAuthorizations is every request the generated REST handlers send the policy, one
// row per served operation, read from the same derivation (httpgen.PlanOps) the handlers and
// the generated MCP tools authorize with, so it covers both transports. The policy
// (Hooks.PolicyDecisionPoint) must admit every row for the authenticated principal's own scope.
`

const authzTestBody = `
import (
	"context"
	"testing"

	security "github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
)

// TestPolicyAdmitsEveryGeneratedRequest holds the composition root's policy to the requests the
// generated transports really send it: for every row of generatedAuthorizations, the local dev
// principal the DevConfig hook gives is permitted that verb on its own scope, asked through
// edgeSecurity, the Security every generated REST handler and MCP tool holds. A policy that
// refuses one of them would refuse that request in the browser and over MCP alike.
//
// The hooks are a zero appHooks, as main would hold before it builds anything: the policy and
// DevConfig must be answerable without the state main builds.
func TestPolicyAdmitsEveryGeneratedRequest(t *testing.T) {
	var hooks Hooks = new(appHooks)
	cfg := &Config{DevPrincipal: "generated-authz-dev-principal"}
	principal := hooks.DevConfig(cfg).Principal
	if principal.Subject == "" {
		t.Fatal("DevConfig gives no principal for the local profile's dev subject, so the policy cannot be held to the generated requests")
	}
	sec := edgeSecurity(hooks)
	for _, row := range generatedAuthorizations {
		decision, err := sec.Authorize(context.Background(), principal,
			security.Action{Verb: row.Verb},
			security.ResourceRef{Kind: row.Scope, ID: principal.Subject})
		if err != nil || !decision.Permit {
			t.Errorf("%s.%s: the policy refuses the request the generated transports send (verb %q on the principal's own %s, subject %q): permit=%v err=%v",
				row.Manager, row.Op, row.Verb, row.Scope, principal.Subject, decision.Permit, err)
		}
	}
}
`
