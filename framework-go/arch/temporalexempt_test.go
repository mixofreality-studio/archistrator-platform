package arch

import "testing"

// TestTemporalExempt locks the suffix-anchored matching of the Temporal-isolation
// allowlist. The exemption MUST be narrow: it matches the named package and only
// the named package — never a sibling that merely shares a prefix, never a
// substring elsewhere in the path. This is the safety property that keeps the
// single sanctioned exception (durableExecutionAccess) from silently widening to
// cover an unintended package.
func TestTemporalExempt(t *testing.T) {
	const da = "resourceaccess/durableexecution"
	cases := []struct {
		name    string
		pkgPath string
		exempt  []string
		want    bool
	}{
		{"exact-suffix match", "github.com/x/server/internal/resourceaccess/durableexecution", []string{da}, true},
		{"equal whole path", da, []string{da}, true},
		{"sibling RA not exempted", "github.com/x/server/internal/resourceaccess/projectstate", []string{da}, false},
		{"prefix-collision not exempted", "github.com/x/server/internal/resourceaccess/durableexecutionx", []string{da}, false},
		{"substring-in-middle not exempted", "github.com/x/resourceaccess/durableexecution/nested", []string{da}, false},
		{"empty allowlist exempts nothing", "github.com/x/server/internal/resourceaccess/durableexecution", nil, false},
		{"empty entry ignored", "github.com/x/server/internal/resourceaccess/durableexecution", []string{""}, false},
		{"unrelated package", "github.com/x/server/internal/utility/security", []string{da}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := temporalExempt(c.pkgPath, c.exempt); got != c.want {
				t.Errorf("temporalExempt(%q, %v) = %v, want %v", c.pkgPath, c.exempt, got, c.want)
			}
		})
	}
}
