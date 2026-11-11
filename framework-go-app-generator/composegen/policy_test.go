package composegen_test

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/composegen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/configgen"
)

var todomvcServerCfg = composegen.Config{ContainerKey: "server", ModulePath: "example.com/todomvc", PackageName: "main", EnvPrefix: "APP"}

// authzRowRe reads one (verb, scope) of authz.gen.go's table.
var authzRowRe = regexp.MustCompile(`Verb: "([^"]+)", Scope: "([^"]+)"\}`)

// policyRows is the `{verb: V, scope: S}: true,` lines of a policy.gen.go, in file order.
func policyRows(policy []byte) []string {
	var rows []string
	for _, line := range strings.Split(string(policy), "\n") {
		// gofmt aligns the map's values; compare the row, not the padding.
		if line = strings.Join(strings.Fields(line), " "); strings.HasPrefix(line, "{verb:") {
			rows = append(rows, line)
		}
	}
	return rows
}

// TestPolicyGen_RowsFromAuthzTable: the composition root's policy is the authorization table
// the generated transports are derived from (Plan 3 Task 3.4), so policy.gen.go holds one
// `{verb: V, scope: S}: true,` per distinct row of authz.gen.go and nothing else.
func TestPolicyGen_RowsFromAuthzTable(t *testing.T) {
	m, _ := loadConformance(t, "todomvc")
	got, err := composegen.Generate(m, todomvcServerCfg)
	if err != nil {
		t.Fatal(err)
	}
	policy, ok := got[composegen.PolicyFile]
	if !ok {
		t.Fatalf("Generate emitted no %s: %v", composegen.PolicyFile, keysOf(got))
	}
	want := map[string]bool{}
	for _, row := range authzRowRe.FindAllStringSubmatch(string(got[composegen.AuthzTableFile]), -1) {
		want[fmt.Sprintf("{verb: %q, scope: %q}: true,", row[1], row[2])] = true
	}
	if !want[`{verb: "add-todo", scope: "listCatalog"}: true,`] {
		t.Fatalf("the table has no add-todo on listCatalog; the fixture changed: %v", want)
	}
	rows := policyRows(policy)
	have := map[string]bool{}
	for _, row := range rows {
		have[row] = true
	}
	for row := range want {
		if !have[row] {
			t.Errorf("policy.gen.go lacks %s:\n%s", row, policy)
		}
	}
	for row := range have {
		if !want[row] {
			t.Errorf("policy.gen.go has %s, which no generated request asks", row)
		}
	}
	if len(rows) != len(have) {
		t.Errorf("policy.gen.go lists a row twice:\n%v", rows)
	}
	// Sorted, so a regeneration is byte-stable.
	if !slices.IsSorted(rows) {
		t.Errorf("policy rows are not sorted:\n%v", rows)
	}
}

// TestPolicyGen_HooksInterfaceHasNoPDP: nothing about the policy is the app's to write.
func TestPolicyGen_HooksInterfaceHasNoPDP(t *testing.T) {
	m := loadGreenfield(t)
	hooks, err := composegen.HookList(m, greenfieldCfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hooks {
		if h.Name == "PolicyDecisionPoint" {
			t.Errorf("the Hooks interface still declares PolicyDecisionPoint: %+v", h)
		}
	}
	got, err := composegen.Generate(m, greenfieldCfg)
	if err != nil {
		t.Fatal(err)
	}
	main := string(got[composegen.MainFile])
	if !strings.Contains(main, "edgeSecurity()") || strings.Contains(main, "hooks.PolicyDecisionPoint") {
		t.Errorf("main.gen.go must build the edge from the generated policy alone:\n%s", main)
	}
}

// TestPolicyGen_TestFileHoldsOwnerAndNonOwner: the generated test holds the policy to both
// halves of its claim, the requests it admits and the ones it refuses.
func TestPolicyGen_TestFileHoldsOwnerAndNonOwner(t *testing.T) {
	m := loadGreenfield(t)
	got, err := composegen.Generate(m, greenfieldCfg)
	if err != nil {
		t.Fatal(err)
	}
	test := string(got[composegen.AuthzTestFile])
	for _, w := range []string{
		"func TestPolicyAdmitsEveryGeneratedRequest(",
		"func TestPolicyGen_AdmitsEveryGeneratedRequestForOwner(",
		"func TestPolicyGen_DeniesNonOwner(",
	} {
		if !strings.Contains(test, w) {
			t.Errorf("the generated policy test lacks %q:\n%s", w, test)
		}
	}
}

// TestPolicyGen_CompilesAndPasses runs the generated policy and its generated tests in the
// compile sandbox: they pass as emitted, and the same policy missing one row of its own table
// fails the test naming that request (FIX-046: a policy that refuses a request the transports
// send is a 403 in the browser).
func TestPolicyGen_CompilesAndPasses(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping compile sandbox in -short")
	}
	m := loadGaps(t)
	main, err := composegen.Generate(m, greenfieldCfg)
	if err != nil {
		t.Fatalf("composegen: %v", err)
	}
	dir := t.TempDir()
	cfgFile, err := configgen.Generate(m, configgen.Config{ContainerKey: "order-app", EnvPrefix: "ORDERAPP", PackageName: "main"})
	if err != nil {
		t.Fatalf("configgen: %v", err)
	}
	files := sandboxFiles()
	for name, src := range main {
		files[name] = string(src)
	}
	files["config.gen.go"] = string(cfgFile["config.gen.go"])
	writeTree(t, dir, files)
	buildSandbox(t, dir)
	if out, err := sandboxGo(dir, "test", "-count=1", "-run", "TestPolicy", "."); err != nil {
		t.Fatalf("the generated policy tests fail as emitted: %v\n%s", err, out)
	}

	policy := string(main[composegen.PolicyFile])
	row := authzRowRe.FindStringSubmatch(string(main[composegen.AuthzTableFile]))
	if row == nil {
		t.Fatalf("no row in the table:\n%s", main[composegen.AuthzTableFile])
	}
	needle := regexp.MustCompile(fmt.Sprintf(`(?m)^\s*\{verb: %q, scope: %q\}:\s+true,\n`, row[1], row[2]))
	if !needle.MatchString(policy) {
		t.Fatalf("policy.gen.go lacks %v:\n%s", needle, policy)
	}
	writeTree(t, dir, map[string]string{composegen.PolicyFile: needle.ReplaceAllString(policy, "")})
	out, err := sandboxGo(dir, "test", "-count=1", "-run", "TestPolicyAdmitsEveryGeneratedRequest", ".")
	if err == nil || !strings.Contains(out, `verb "`+row[1]+`"`) {
		t.Fatalf("a policy missing %q passed the generated test: %v\n%s", needle, err, out)
	}
}

func keysOf(m map[string][]byte) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
}
