package httpgen

import (
	"strings"

	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// placement is where an operation parameter is carried on the wire.
type placement int

const (
	placePath placement = iota
	placeBody
	placeQuery
)

// paramPlan is one parameter with its resolved wire placement.
type paramPlan struct {
	param projectmodel.Param
	place placement
}

// opPlan is a fully resolved HTTP binding for one operation.
type opPlan struct {
	op          projectmodel.Operation
	method      string // "GET" or "POST"
	path        string // /api/v1/<manager>/<op>[/{param}...]
	pathParams  []paramPlan
	bodyParams  []paramPlan
	queryParams []paramPlan
	verb        string // kebab(opName), the authz Action.Verb
	scope       string // the owner scope's authz ResourceRef.Kind: <manager>Catalog
}

// planOps resolves the HTTP binding for every operation per the archistrator
// client convention.
//
// CONVENTION (and the ambiguities decided here):
//   - method: an op is GET only when its NAME is a read (starts Get/List/Query)
//     AND every NON-path param is a scalar (string / integer / number / boolean /
//     uuid — representable as a path or query value). Otherwise it is POST. A
//     read whose param set includes an object/array CANNOT be GET (a struct can't
//     ride the query string), so it falls through to POST.
//   - PATH param: a param that names the resource identity — either (a) a $ref to
//     a STRING-SCALAR $def whose TYPE name ends in "ID" (ProjectID, ActivityID),
//     or (b) an inline uuid (x-go-type uuid.UUID) whose PARAM name ends in "ID"
//     (operatedAppID, customerID). This keeps non-ID named scalars (OwnerScope)
//     and plain inline strings (requestID, tickID) OUT of the URL — matching the
//     real server, where the owner is the principal and correlation tokens are
//     carried in body/query.
//   - remaining params: GET -> query (each a scalar); POST -> JSON request body
//     (a wrapper object with one field per non-path param, scalar or not).
//   - path: /api/v1/<manager-kebab>/<op-kebab> then /{param} per ID path param
//     in declared order.
//   - authz: Verb = kebab(opName), and EVERY request is owner-scoped: the
//     resource is the principal's own catalog (Kind=<manager>Catalog,
//     ID=principal.Subject), whatever params the op carries. Who owns a resource
//     the op names (a todo, a project) is business data the Manager resolves
//     through owner-scoped ResourceAccess verbs; the edge never guesses a
//     resource kind from a parameter's name, so the policy's vocabulary is
//     exactly (verb, own scope) and nothing a hand-written policy must guess.
func planOps(doc *projectmodel.Doc) []opPlan {
	mgrKebab := projectmodel.Kebab(doc.ManagerBase())
	plans := make([]opPlan, 0, len(doc.Interface.Operations))
	for _, op := range doc.Interface.Operations {
		plans = append(plans, planOp(doc, op, mgrKebab))
	}
	return plans
}

// planOp resolves the HTTP binding for a single operation per the convention
// documented on planOps.
func planOp(doc *projectmodel.Doc, op projectmodel.Operation, mgrKebab string) opPlan {
	pathParams, rest := splitParams(doc, op.Params)

	method := "POST"
	if methodFor(op.Name) == "GET" && allScalar(doc, rest) {
		method = "GET"
	}

	p := opPlan{op: op, method: method, verb: projectmodel.Kebab(op.Name), scope: OwnerScopeKind(doc)}
	path := "/api/v1/" + mgrKebab + "/" + projectmodel.Kebab(op.Name)
	for _, param := range pathParams {
		p.pathParams = append(p.pathParams, paramPlan{param, placePath})
		path += "/{" + param.Name + "}"
	}
	assignRest(&p, rest, method)
	p.path = path
	return p
}

// OwnerScopeKind is the authz ResourceRef.Kind every generated request of doc's
// Manager names: the principal's own catalog, <manager>Catalog (e.g.
// "listCatalog" for ListManager). The ResourceRef.ID is always the principal's
// subject.
func OwnerScopeKind(doc *projectmodel.Doc) string {
	return projectmodel.LowerFirst(doc.ManagerBase()) + "Catalog"
}

// splitParams partitions an op's params into ID path params and the rest, in
// declared order.
func splitParams(doc *projectmodel.Doc, params []projectmodel.Param) (pathParams, rest []projectmodel.Param) {
	for _, param := range params {
		if isIDPathParam(doc, param) {
			pathParams = append(pathParams, param)
		} else {
			rest = append(rest, param)
		}
	}
	return pathParams, rest
}

// assignRest places each non-path param onto the wire: query for a GET, JSON body
// otherwise.
func assignRest(p *opPlan, rest []projectmodel.Param, method string) {
	for _, param := range rest {
		if method == "GET" {
			p.queryParams = append(p.queryParams, paramPlan{param, placeQuery})
		} else {
			p.bodyParams = append(p.bodyParams, paramPlan{param, placeBody})
		}
	}
}

func methodFor(opName string) string {
	for _, p := range []string{"Get", "List", "Query"} {
		if strings.HasPrefix(opName, p) {
			return "GET"
		}
	}
	return "POST"
}

// allScalar reports whether every param can ride a path/query value.
func allScalar(doc *projectmodel.Doc, params []projectmodel.Param) bool {
	for _, p := range params {
		if _, ok := doc.ScalarKind(p.Schema); !ok {
			return false
		}
	}
	return true
}

func isIDPathParam(doc *projectmodel.Doc, p projectmodel.Param) bool {
	if name := p.Schema.RefName(); name != "" {
		return doc.IsScalarStringDef(name) && projectmodel.EndsWithID(name)
	}
	// Inline uuid identity (operatedAppID, customerID): keyed on the param name.
	if kind, ok := doc.ScalarKind(p.Schema); ok && kind == "uuid" {
		return projectmodel.EndsWithID(p.Name)
	}
	return false
}
