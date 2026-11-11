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

// emitAuthz renders authz.gen.go (the table) and authz.gen_test.go (the tests that hold the
// composition root's generated policy (policy.gen.go) to it). The tests are the framework's, not
// the app's: they reach the policy through the generated edgeSecurity, so no app writes a probe of
// its own, and a policy that refuses a request the generated transports send, or admits one they
// do not, goes red in the activity that composes the server.
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
// the generated MCP tools authorize with, so it covers both transports. The composition root's
// generated policy (policy.gen.go) is derived from these rows: it admits every row for the
// authenticated principal's own scope.
`

const authzTestBody = `
import (
	"context"
	"testing"

	security "github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
)

// TestPolicyAdmitsEveryGeneratedRequest holds the composition root's policy to the requests the
// generated transports really send it: for every row of generatedAuthorizations, an authenticated
// principal is permitted that verb on its own scope, asked through edgeSecurity, the Security
// every generated REST handler and MCP tool holds. A policy that refused one of them would
// refuse that request in the browser and over MCP alike.
func TestPolicyAdmitsEveryGeneratedRequest(t *testing.T) {
	principal := security.Principal{Subject: "owner-a"}
	sec := edgeSecurity()
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

// TestPolicyGen_AdmitsEveryGeneratedRequestForOwner: the policy reads no role and no principal
// kind, so every generated request is admitted for a user and an application principal alike,
// with no roles and with an agent role.
func TestPolicyGen_AdmitsEveryGeneratedRequestForOwner(t *testing.T) {
	sec := edgeSecurity()
	for _, kind := range []security.PrincipalKind{security.PrincipalUser, security.PrincipalApplication} {
		for _, roles := range [][]string{{}, {"agent"}} {
			principal := security.Principal{Kind: kind, Subject: "owner-a", Roles: roles}
			for _, row := range generatedAuthorizations {
				decision, err := sec.Authorize(context.Background(), principal,
					security.Action{Verb: row.Verb},
					security.ResourceRef{Kind: row.Scope, ID: principal.Subject})
				if err != nil || !decision.Permit {
					t.Errorf("%s.%s: refused for a %s principal with roles %v (verb %q on %s): permit=%v err=%v",
						row.Manager, row.Op, kind, roles, row.Verb, row.Scope, decision.Permit, err)
				}
			}
		}
	}
}

// TestPolicyGen_DeniesNonOwner: the policy admits nothing but the generated requests on the
// principal's own scope. Another subject's scope, an unauthenticated principal, a verb no
// transport sends and a scope no transport asks are each refused.
func TestPolicyGen_DeniesNonOwner(t *testing.T) {
	sec := edgeSecurity()
	owner := security.Principal{Subject: "owner-a"}
	denied := func(p security.Principal, verb, scope, id, why string) {
		t.Helper()
		decision, err := sec.Authorize(context.Background(), p, security.Action{Verb: verb}, security.ResourceRef{Kind: scope, ID: id})
		if err == nil && decision.Permit {
			t.Errorf("the policy permits %s: verb %q on %s %q for subject %q", why, verb, scope, id, p.Subject)
		}
	}
	for _, row := range generatedAuthorizations {
		denied(owner, row.Verb, row.Scope, "owner-b", row.Manager+"."+row.Op+" on another subject's scope")
		denied(security.Principal{}, row.Verb, row.Scope, "", row.Manager+"."+row.Op+" for no subject")
		denied(owner, row.Verb+"-unknown", row.Scope, owner.Subject, "a verb no transport sends")
		denied(owner, row.Verb, row.Scope+"Unknown", owner.Subject, "a scope no transport asks")
	}
}
`
