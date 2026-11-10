package configgen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/configgen"
)

// sandboxGoMod is the go.mod of a throwaway module holding a generated config.gen.go: the file
// reads the local dev principals as the framework's security.Principal, so the module requires
// framework-go, resolved to the sibling checkout this test runs from.
func sandboxGoMod(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Skip("cannot locate the framework-go sibling (runtime.Caller)")
	}
	fw := filepath.Join(filepath.Dir(file), "..", "..", "framework-go")
	return "module configsandbox\n\ngo 1.25\n\n" +
		"require github.com/mixofreality-studio/archistrator-platform/framework-go v0.0.0\n\n" +
		"replace github.com/mixofreality-studio/archistrator-platform/framework-go => " + fw + "\n"
}

// localDevPrincipalsProbe runs inside the sandbox against the generated rule.
const localDevPrincipalsProbe = `package config

import (
	"strings"
	"testing"
)

func TestLocalDevPrincipals(t *testing.T) {
	const list = ` + "`" + `[{"subject":"alice","roles":["owner"]},{"subject":"bot","kind":"application","claims":{"delegations":[{"grantor":"alice","intents":["read"]}]}}]` + "`" + `
	for _, c := range []struct {
		env, profile string
		want         int
		wantErr      string
	}{
		{"", "local", 0, ""},
		{"", "cloud", 0, ""},
		{list, "local", 2, ""},
		{list, "cloud", 0, "ORDERAPP_DEV_PRINCIPALS"},
		{"not json", "local", 0, "ORDERAPP_DEV_PRINCIPALS"},
		{` + "`" + `[{"subject":"a"},{"subject":"a"}]` + "`" + `, "local", 0, "twice"},
		{` + "`" + `[{"subject":""}]` + "`" + `, "local", 0, "subject"},
	} {
		t.Setenv("ORDERAPP_DEV_PRINCIPALS", c.env)
		t.Setenv("ORDER_TEMPORAL_ADDR", "localhost:7233")
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatal(err)
		}
		got, err := cfg.LocalDevPrincipals(c.profile)
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("%q under %q: want an error naming %q, got %v", c.env, c.profile, c.wantErr, err)
			}
			continue
		}
		if err != nil || len(got) != c.want {
			t.Errorf("%q under %q: got %d principals, %v; want %d", c.env, c.profile, len(got), err, c.want)
		}
	}
	t.Setenv("ORDERAPP_DEV_PRINCIPALS", list)
	t.Setenv("ORDER_TEMPORAL_ADDR", "localhost:7233")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	got, err := cfg.LocalDevPrincipals("local")
	if err != nil {
		t.Fatal(err)
	}
	if a := got["alice"]; a.Subject != "alice" || len(a.Roles) != 1 || a.Roles[0] != "owner" || a.Kind != "user" {
		t.Errorf("alice = %+v", a)
	}
	if b := got["bot"]; b.Kind != "application" || b.Claims["delegations"] == nil {
		t.Errorf("bot = %+v", b)
	}
}
`

// TestConfig_LocalDevPrincipals: the local-profile principal allowlist (<PREFIX>_DEV_PRINCIPALS,
// Plan 3 Task 3.3, spec 4b) is read by one generated rule, as the single dev principal is: a JSON
// list of {subject, kind, roles, claims}, honoured only under the local profile, refused by name
// under any other, an error for malformed JSON or a subject listed twice or empty.
func TestConfig_LocalDevPrincipals(t *testing.T) {
	m := loadGreenfield(t)
	out, err := configgen.Generate(m, greenfieldCfg)
	if err != nil {
		t.Fatal(err)
	}
	src := string(out["config.gen.go"])
	for _, want := range []string{
		"func (c *Config) LocalDevPrincipals(profile string) (map[string]security.Principal, error)",
		`const devPrincipalsEnv = "ORDERAPP_DEV_PRINCIPALS"`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("config.gen.go lacks %q:\n%s", want, src)
		}
	}
	if got := configgen.DevPrincipalsEnv("ORDERAPP"); got != "ORDERAPP_DEV_PRINCIPALS" {
		t.Errorf("DevPrincipalsEnv = %q", got)
	}
	if testing.Short() {
		t.Skip("skipping the compiled rule in -short")
	}
	dir := t.TempDir()
	for name, body := range map[string][]byte{
		"go.mod":                []byte(sandboxGoMod(t)),
		"config.gen.go":         out["config.gen.go"],
		"devprincipals_test.go": []byte(localDevPrincipalsProbe),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	if o, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the generated dev-principals rule failed: %v\n%s", err, o)
	}
}
