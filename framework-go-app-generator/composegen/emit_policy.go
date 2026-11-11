package composegen

import (
	"fmt"
	"go/format"
	"slices"
	"strings"
)

// emitPolicy renders policy.gen.go: the composition root's authorization policy, derived from
// the same authorization rows (authzRows) the generated transports are. Every generated request
// is owner-scoped (httpgen.OpPlan.AuthzScope is the principal's own <manager>Catalog and the
// ResourceRef.ID is always the principal's subject), so the policy has nothing to express beyond
// the distinct (verb, scope) pairs the transports send: it permits an authenticated principal
// exactly those on its own scope and denies everything else. Resource ownership inside an
// operation (whose list, whose delegation) is the Manager's to decide, never this policy's.
func emitPolicy(r *resolved) ([]byte, error) {
	rows, err := authzRows(r)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var lines []string
	for _, row := range rows {
		line := fmt.Sprintf("\t{verb: %q, scope: %q}: true,\n", row.verb, row.scope)
		if !seen[line] {
			seen[line] = true
			lines = append(lines, line)
		}
	}
	slices.Sort(lines)

	var b strings.Builder
	b.WriteString(genHeader)
	b.WriteString("\npackage " + r.pkgName + "\n\n")
	b.WriteString(policyImports)
	b.WriteString(policyTypeDoc)
	b.WriteString("type authzKey struct{ verb, scope string }\n\n")
	b.WriteString(policyRowsDoc)
	b.WriteString("var generatedPolicyRows = map[authzKey]bool{\n")
	for _, l := range lines {
		b.WriteString(l)
	}
	b.WriteString("}\n\n")
	b.WriteString(policyDecideSource)
	src, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, wrapFormatErr(err, b.String())
	}
	return src, nil
}

const policyImports = `import (
	"context"

	security "` + pathSecurity + `"
)

`

const policyTypeDoc = `// authzKey is one decision a generated transport asks the policy: a verb on a scope (the
// security.ResourceRef.Kind).
`

const policyRowsDoc = `// generatedPolicyRows is every (verb, scope) the generated REST handlers and MCP tools send the
// policy, one entry per distinct row of generatedAuthorizations (authz.gen.go).
`

const policyDecideSource = `// generatedPolicy is the composition root's authorization policy, derived from the model: it
// permits an authenticated principal exactly the requests the generated transports send, on the
// principal's own scope (ResourceRef.ID == Subject), and denies everything else. It reads no
// role: a principal's reach is the Manager's to decide (ownership, delegation), not the edge's.
type generatedPolicy struct{}

func (generatedPolicy) Decide(_ context.Context, p security.Principal, a security.Action, r security.ResourceRef) (bool, error) {
	return p.Subject != "" && r.ID == p.Subject && generatedPolicyRows[authzKey{a.Verb, r.Kind}], nil
}
`
