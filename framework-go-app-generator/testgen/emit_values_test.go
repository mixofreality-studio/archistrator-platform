package testgen

import (
	"regexp"
	"strings"
	"testing"
)

// TestJSONValueFollowsMethodcheck pins jsonValue to TP-ARG-TYPE's reading of a
// binding value: a string-typed parameter's value is embedded as-is when it is
// already a JSON string literal, and quoted when it is raw text — even raw text
// that happens to be valid JSON of another kind (a number, an object).
func TestJSONValueFollowsMethodcheck(t *testing.T) {
	for _, tc := range []struct {
		value       string
		stringTyped bool
		want        string
	}{
		{`"abc"`, true, `"abc"`},
		{` "abc"`, true, ` "abc"`},
		{`abc`, true, `"abc"`},
		{`42`, true, `"42"`},
		{`{"a":1}`, true, `"{\"a\":1}"`},
		{`42`, false, `42`},
		{`{"a":1}`, false, `{"a":1}`},
		{`"abc"`, false, `"abc"`},
		{`not json`, false, `"not json"`},
	} {
		if got := jsonValue(tc.value, tc.stringTyped); got != tc.want {
			t.Errorf("jsonValue(%q, stringTyped=%v) = %s, want %s", tc.value, tc.stringTyped, got, tc.want)
		}
	}
}

// TestURLMatcherMatchesTheRoute pins the `url` observable to the route it names:
// the SPA's selection query and the origin that served it never fail the
// assertion, a different path always does, and an observable that names a query
// or an absolute URL is matched whole.
func TestURLMatcherMatchesTheRoute(t *testing.T) {
	const route = "/project/dct-owner%2Fdct-delivery/activity/C-review-engine"
	m := urlMatcher(route)
	const pre, post = "new RegExp('", "')"
	if !strings.HasPrefix(m, pre) || !strings.HasSuffix(m, post) {
		t.Fatalf("urlMatcher(%q) = %s, want a RegExp", route, m)
	}
	// The TS literal escapes backslashes only; undo that to get the pattern JS sees.
	re := regexp.MustCompile(strings.ReplaceAll(strings.TrimSuffix(strings.TrimPrefix(m, pre), post), `\\`, `\`))
	for _, u := range []string{
		"http://localhost:5173" + route,
		"http://127.0.0.1:4173" + route + "?task=draft&rev=3",
		"https://app.example" + route + "#comments",
	} {
		if !re.MatchString(u) {
			t.Errorf("%s does not match %s", m, u)
		}
	}
	for _, u := range []string{
		"http://localhost:5173" + route + "/extra",
		"http://localhost:5173/prefix" + route,
		"http://localhost:5173/project/dct-owner%2Fdct-delivery/activity/C-review-engineX",
	} {
		if re.MatchString(u) {
			t.Errorf("%s must not match %s", m, u)
		}
	}
	for _, exact := range []string{"/p?task=x", "https://app.example/p"} {
		if got, want := urlMatcher(exact), tsStr(exact); got != want {
			t.Errorf("urlMatcher(%q) = %s, want the exact URL %s", exact, got, want)
		}
	}
}
