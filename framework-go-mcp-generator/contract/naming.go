package contract

import "strings"

// Kebab converts a PascalCase / camelCase identifier to kebab-case
// (e.g. "ExecuteNextActivity" -> "execute-next-activity", "GetProject" ->
// "get-project").
func Kebab(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if isUpper(r) {
			prevLower := i > 0 && !isUpper(runes[i-1])
			nextLower := i+1 < len(runes) && !isUpper(runes[i+1])
			if i > 0 && (prevLower || nextLower) {
				b.WriteByte('-')
			}
			b.WriteRune(toLower(r))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// LowerFirst lowercases only the first rune (e.g. "Project" -> "project",
// "GetProject" -> "getProject").
func LowerFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = toLower(r[0])
	return string(r)
}

// TrimIDSuffix removes a trailing "ID" (e.g. "ProjectID" -> "Project").
func TrimIDSuffix(s string) string { return strings.TrimSuffix(s, "ID") }

// EndsWithID reports whether a type name ends in "ID" (the ID-ish test).
func EndsWithID(s string) bool { return strings.HasSuffix(s, "ID") }

func isUpper(r rune) bool { return r >= 'A' && r <= 'Z' }
func toLower(r rune) rune {
	if isUpper(r) {
		return r + ('a' - 'A')
	}
	return r
}
