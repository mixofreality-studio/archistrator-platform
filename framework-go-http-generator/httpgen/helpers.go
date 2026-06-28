package httpgen

import "strings"

// writeHelpers emits the per-file response/error helpers, matching the
// archistrator server recipe (decodeJSON with DisallowUnknownFields, the
// {error,code} envelope, and the manager.Kind -> HTTP status / wire-code maps).
func writeHelpers(b *strings.Builder) {
	b.WriteString(helpersSrc)
}

const helpersSrc = `// --- response helpers ------------------------------------------------------

type errorResponse struct {
	Error string ` + "`json:\"error\"`" + `
	Code  string ` + "`json:\"code\"`" + `
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, detail string) {
	writeJSON(w, status, errorResponse{Error: detail, Code: code})
}

// writeManagerError maps a framework-go manager.Error to its HTTP status; any
// other error is a non-leaking 500.
func writeManagerError(w http.ResponseWriter, err error) {
	var me *fwmanager.Error
	if errors.As(err, &me) {
		writeError(w, statusForKind(me.Kind), codeForKind(me.Kind), me.Detail)
		return
	}
	writeError(w, http.StatusInternalServerError, "internal", "internal error")
}

func statusForKind(kind fwmanager.Kind) int {
	switch kind {
	case fwmanager.ContractMisuse:
		return http.StatusBadRequest
	case fwmanager.NotFound:
		return http.StatusNotFound
	case fwmanager.Unauthorized:
		return http.StatusForbidden
	case fwmanager.FailedPrecondition:
		return http.StatusConflict
	case fwmanager.Infrastructure:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func codeForKind(kind fwmanager.Kind) string {
	switch kind {
	case fwmanager.ContractMisuse:
		return "contract_misuse"
	case fwmanager.NotFound:
		return "not_found"
	case fwmanager.Unauthorized:
		return "forbidden"
	case fwmanager.FailedPrecondition:
		return "failed_precondition"
	case fwmanager.Infrastructure:
		return "infrastructure"
	default:
		return "internal"
	}
}
`
