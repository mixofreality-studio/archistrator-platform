package configgen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/configgen"
)

// localScenariohostURLProbe runs inside the sandbox against the generated rule.
const localScenariohostURLProbe = `package config

import (
	"strings"
	"testing"
)

func TestLocalScenariohostURL(t *testing.T) {
	for _, c := range []struct {
		env, profile, want, wantErr string
	}{
		{"", "local", "", ""},
		{"", "cloud", "", ""},
		{"http://127.0.0.1:9", "local", "http://127.0.0.1:9", ""},
		{"http://127.0.0.1:9", "cloud", "", "honoured only under the local profile"},
	} {
		t.Setenv("ORDERAPP_SCENARIOHOST_URL", c.env)
		t.Setenv("ORDER_TEMPORAL_ADDR", "localhost:7233")
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatal(err)
		}
		got, err := cfg.LocalScenariohostURL(c.profile)
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) || !strings.Contains(err.Error(), "ORDERAPP_SCENARIOHOST_URL") {
				t.Errorf("%q under %q: want an error naming the variable and %q, got %v", c.env, c.profile, c.wantErr, err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%q under %q: got %q, %v; want %q", c.env, c.profile, got, err, c.want)
		}
	}
}
`

// TestConfig_LocalScenariohostURL: the address of the scenario host's arrange server
// (<PREFIX>_SCENARIOHOST_URL, Plan 3 Task 3.5) is read by one generated rule, as the dev principal
// is: honoured only under the local profile, refused by name under any other.
func TestConfig_LocalScenariohostURL(t *testing.T) {
	out, err := configgen.Generate(loadGreenfield(t), greenfieldCfg)
	if err != nil {
		t.Fatal(err)
	}
	src := string(out["config.gen.go"])
	for _, want := range []string{
		"func (c *Config) LocalScenariohostURL(profile string) (string, error)",
		`const scenariohostURLEnv = "ORDERAPP_SCENARIOHOST_URL"`,
		"honoured only under the local profile",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("config.gen.go lacks %q:\n%s", want, src)
		}
	}
	if got := configgen.ScenariohostURLEnv("ORDERAPP"); got != "ORDERAPP_SCENARIOHOST_URL" {
		t.Errorf("ScenariohostURLEnv = %q", got)
	}
	if testing.Short() {
		t.Skip("skipping the compiled rule in -short")
	}
	dir := t.TempDir()
	for name, body := range map[string][]byte{
		"go.mod":                  []byte(sandboxGoMod(t)),
		"config.gen.go":           out["config.gen.go"],
		"scenariohosturl_test.go": []byte(localScenariohostURLProbe),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	if o, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the generated scenariohost URL rule failed: %v\n%s", err, o)
	}
}
