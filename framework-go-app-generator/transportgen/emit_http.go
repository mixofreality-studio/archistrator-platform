package transportgen

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	httpgen "github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator/httpgen"
	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// emitManagerHTTP emits http_<mgr>.gen.go: one typed method on the shared
// HTTPClient per operation, plus a request-wrapper struct per POST op.
func emitManagerHTTP(info *mgrInfo, cfg Config) ([]byte, error) {
	var body bytes.Buffer
	needFmt, needURL := false, false
	for _, op := range info.doc.Interface.Operations {
		f, u := writeHTTPMethod(&body, info, op, info.plans[op.Name], cfg)
		needFmt = needFmt || f
		needURL = needURL || u
	}

	var out bytes.Buffer
	out.WriteString(genHeader)
	fmt.Fprintf(&out, "package %s\n\n", cfg.PackageName)
	imports := []string{"context", "net/http"}
	if needFmt {
		imports = append(imports, "fmt")
	}
	if needURL {
		imports = append(imports, "net/url")
	}
	writeImports(&out, imports)
	out.Write(body.Bytes())
	return formatFile(out.String())
}

func writeHTTPMethod(b *bytes.Buffer, info *mgrInfo, op projectmodel.Operation, plan httpgen.OpPlan, cfg Config) (needFmt, needURL bool) {
	method := info.base + op.Name
	paramByName := map[string]projectmodel.Param{}
	for _, p := range op.Params {
		paramByName[p.Name] = p
	}
	pathParams := pathParamSet(plan)

	// Request wrapper for POST ops (always sent, even when empty — the server
	// handler always decodes a body).
	isPost := plan.Verb == "POST"
	reqType := method + "Request"
	if isPost {
		fmt.Fprintf(b, "// %s is the JSON request body for %s.\n", reqType, method)
		fmt.Fprintf(b, "type %s struct {\n", reqType)
		for _, bp := range plan.BodyParams {
			p := paramByName[bp.Name]
			fmt.Fprintf(b, "\t%s %s `json:%q`\n", upperFirst(bp.Name),
				goType(p.Schema, p.Pointer, info.rename, cfg.UUIDAsString), jsonTag(bp.Name, p.Pointer))
		}
		b.WriteString("}\n\n")
	}

	sig := buildMethodSignature(op, pathParams, info, cfg)
	resultType := goType(op.Result, false, info.rename, cfg.UUIDAsString)

	fmt.Fprintf(b, "// %s calls the %s operation on the %s manager over HTTP.\n", method, op.Name, info.base)
	if plan.HasResult {
		fmt.Fprintf(b, "func (c *HTTPClient) %s(%s) (%s, error) {\n", method, strings.Join(sig, ", "), resultType)
	} else {
		fmt.Fprintf(b, "func (c *HTTPClient) %s(%s) error {\n", method, strings.Join(sig, ", "))
	}

	// Path assembly.
	nf, nu := writePathAssembly(b, plan)
	needFmt, needURL = nf, nu

	// Request/response.
	switch {
	case isPost && plan.HasResult:
		fmt.Fprintf(b, "\tvar out %s\n", resultType)
		fmt.Fprintf(b, "\terr := c.doRequest(ctx, http.MethodPost, path, %s, &out, http.StatusOK)\n", requestLiteral(reqType, plan))
		b.WriteString("\treturn out, err\n")
	case isPost && !plan.HasResult:
		fmt.Fprintf(b, "\treturn c.doRequest(ctx, http.MethodPost, path, %s, nil, http.StatusNoContent)\n", requestLiteral(reqType, plan))
	default: // GET (always has a result)
		fmt.Fprintf(b, "\tvar out %s\n", resultType)
		b.WriteString("\terr := c.doRequest(ctx, http.MethodGet, path, nil, &out, http.StatusOK)\n")
		b.WriteString("\treturn out, err\n")
	}
	b.WriteString("}\n\n")
	return needFmt, needURL
}

// buildMethodSignature renders a client method's Go parameter list. A
// path-positioned param (pathParams) is ALWAYS emitted as a value scalar: the
// URL segment is always present on the wire (the "-" placeholder convention
// is a caller policy, not an absent value), regardless of PlanParam.Pointer —
// see writePathAssembly, which formats it with a plain %s/%d/%v verb.
func buildMethodSignature(op projectmodel.Operation, pathParams map[string]bool, info *mgrInfo, cfg Config) []string {
	sig := []string{"ctx context.Context"}
	for _, p := range op.Params {
		ptr := p.Pointer && !pathParams[p.Name]
		sig = append(sig, p.Name+" "+goType(p.Schema, ptr, info.rename, cfg.UUIDAsString))
	}
	return sig
}

// requestLiteral renders the POST request wrapper composite literal, filling one
// field per body param.
func requestLiteral(reqType string, plan httpgen.OpPlan) string {
	if len(plan.BodyParams) == 0 {
		return reqType + "{}"
	}
	fields := make([]string, 0, len(plan.BodyParams))
	for _, bp := range plan.BodyParams {
		fields = append(fields, upperFirst(bp.Name)+": "+bp.Name)
	}
	return reqType + "{" + strings.Join(fields, ", ") + "}"
}

// pathParamSet returns the set of op param names plan resolves as URL path
// params (plan.PathParams), the client-signature source of truth for which
// params must be emitted as value scalars rather than pointers — a path
// segment is always present on the wire, unlike an optional body/query param.
func pathParamSet(plan httpgen.OpPlan) map[string]bool {
	set := make(map[string]bool, len(plan.PathParams))
	for _, pp := range plan.PathParams {
		set[pp.Name] = true
	}
	return set
}

// writePathAssembly emits the statements assigning `path`, returning whether fmt
// / net/url are needed.
func writePathAssembly(b *bytes.Buffer, plan httpgen.OpPlan) (needFmt, needURL bool) {
	if len(plan.PathParams) == 0 {
		fmt.Fprintf(b, "\tpath := %s\n", strconv.Quote(plan.PathTemplate))
	} else {
		tmpl := plan.PathTemplate
		args := make([]string, 0, len(plan.PathParams))
		for _, pp := range plan.PathParams {
			tmpl = strings.Replace(tmpl, "{"+pp.Name+"}", fmtVerb(pp.ScalarKind), 1)
			args = append(args, pp.Name)
		}
		fmt.Fprintf(b, "\tpath := fmt.Sprintf(%s, %s)\n", strconv.Quote(tmpl), strings.Join(args, ", "))
		needFmt = true
	}
	if len(plan.QueryParams) > 0 {
		needFmt, needURL = true, true
		b.WriteString("\tq := url.Values{}\n")
		for _, qp := range plan.QueryParams {
			// fmt.Sprint (not Sprintf "%s"/"%d") stringifies any scalar kind
			// uniformly and dodges staticcheck S1025 on a plain-string value.
			fmt.Fprintf(b, "\tq.Set(%q, fmt.Sprint(%s))\n", qp.Name, qp.Name)
		}
		b.WriteString("\tpath += \"?\" + q.Encode()\n")
	}
	return needFmt, needURL
}

// fmtVerb is the fmt verb that stringifies a scalar path/query value by its
// wire kind.
func fmtVerb(kind string) string {
	switch kind {
	case "integer":
		return "%d"
	case "number", "boolean":
		return "%v"
	default: // string, uuid (uuid-as-string), or unknown
		return "%s"
	}
}
