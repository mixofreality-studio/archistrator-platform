package httpgen

import (
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator/contract"
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
	param contract.Param
	place placement
}

// opPlan is a fully resolved HTTP binding for one operation.
type opPlan struct {
	op           contract.Operation
	method       string // "GET" or "POST"
	path         string // /api/v1/<manager>/<op>[/{param}...]
	pathParams   []paramPlan
	bodyParams   []paramPlan
	queryParams  []paramPlan
	verb         string // kebab(opName), the authz Action.Verb
	resourceKind string // authz ResourceRef.Kind
	catalog      bool   // true when there is no ID path param (owner-scoped)
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
//   - authz: Verb = kebab(opName); when there is >=1 ID path param the resource
//     is Kind=<identity minus trailing ID, lowercased>, ID=<that value>;
//     otherwise it is the owner-scoped catalog (Kind=<manager>Catalog,
//     ID=principal.Subject).
func planOps(doc *contract.Doc) []opPlan {
	mgrKebab := contract.Kebab(doc.ManagerBase())
	plans := make([]opPlan, 0, len(doc.Interface.Operations))
	for _, op := range doc.Interface.Operations {
		var pathParams, rest []contract.Param
		for _, param := range op.Params {
			if isIDPathParam(doc, param) {
				pathParams = append(pathParams, param)
			} else {
				rest = append(rest, param)
			}
		}

		method := "POST"
		if methodFor(op.Name) == "GET" && allScalar(doc, rest) {
			method = "GET"
		}

		p := opPlan{op: op, method: method, verb: contract.Kebab(op.Name)}
		path := "/api/v1/" + mgrKebab + "/" + contract.Kebab(op.Name)
		for _, param := range pathParams {
			p.pathParams = append(p.pathParams, paramPlan{param, placePath})
			path += "/{" + param.Name + "}"
		}
		for _, param := range rest {
			if method == "GET" {
				p.queryParams = append(p.queryParams, paramPlan{param, placeQuery})
			} else {
				p.bodyParams = append(p.bodyParams, paramPlan{param, placeBody})
			}
		}
		p.path = path
		if len(p.pathParams) > 0 {
			p.resourceKind = resourceKindFor(p.pathParams[0].param)
		} else {
			p.catalog = true
			p.resourceKind = contract.LowerFirst(doc.ManagerBase()) + "Catalog"
		}
		plans = append(plans, p)
	}
	return plans
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
func allScalar(doc *contract.Doc, params []contract.Param) bool {
	for _, p := range params {
		if _, ok := doc.ScalarKind(p.Schema); !ok {
			return false
		}
	}
	return true
}

func isIDPathParam(doc *contract.Doc, p contract.Param) bool {
	if name := p.Schema.RefName(); name != "" {
		return doc.IsScalarStringDef(name) && contract.EndsWithID(name)
	}
	// Inline uuid identity (operatedAppID, customerID): keyed on the param name.
	if kind, ok := doc.ScalarKind(p.Schema); ok && kind == "uuid" {
		return contract.EndsWithID(p.Name)
	}
	return false
}

// resourceKindFor derives the authz resource Kind from the first path param: the
// $def type name for a named scalar, else the param name for an inline uuid, with
// the trailing "ID" stripped and lowercased.
func resourceKindFor(p contract.Param) string {
	if name := p.Schema.RefName(); name != "" {
		return strings.ToLower(contract.TrimIDSuffix(name))
	}
	return strings.ToLower(contract.TrimIDSuffix(p.Name))
}
