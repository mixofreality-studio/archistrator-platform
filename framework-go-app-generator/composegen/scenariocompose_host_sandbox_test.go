package composegen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/composegen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

const platformBase = "github.com/mixofreality-studio/archistrator-platform/"

// stubOrderState stands in for the orderStateAccess component the fixture's contract names: the
// interface the null wrapper has to implement, the types its operations carry, and the
// constructor a config-valued local arm calls.
const stubOrderState = `package orderstate

import fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"

type (
	OrderID      string
	Version      int64
	Order        struct{ ID OrderID }
	OrderReceipt struct{ ID OrderID }
)

type Access interface {
	ReadOrder(rc fwra.Context, orderID OrderID) (Order, error)
	PutOrder(rc fwra.Context, orderID OrderID, expectedVersion Version, order Order) (Version, error)
	CancelOrder(rc fwra.Context, orderID OrderID, expectedVersion Version, reason string) (Version, error)
	ArchiveOrder(rc fwra.Context, orderID OrderID, expectedVersion Version, key fwra.IdempotencyKey) (Version, error)
	ChargeOrder(rc fwra.Context, orderID OrderID, amountCents int64, key fwra.IdempotencyKey) (Version, error)
	PurgeOrder(rc fwra.Context, orderID OrderID) error
	ReceiptForOrder(rc fwra.Context, orderID OrderID) (OrderReceipt, error)
}

func NewGitLocalAccess(repoURL string) (Access, error) { return nil, nil }
`

// TestScenarioCompose_HostInputCompilesAgainstTheRealHost: the file emitted for a config-valued
// local arm (a git-repo infra) is spelled s.h.Input(s.t, key, "git-repo", "URL"); it builds and
// vets against the REAL scenariohost.Host, so an emitter that spells an Input the host lacks (or
// with another signature) fails here, not in the first app that regenerates.
func TestScenarioCompose_HostInputCompilesAgainstTheRealHost(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles the real scenario host")
	}
	m := loadScenarioFixture(t)
	for k := range m.Contracts {
		if k != "orderStateAccess" {
			delete(m.Contracts, k)
		}
	}
	m.Deployment.Infrastructure = append(m.Deployment.Infrastructure,
		projectmodel.InfraDecl{Key: "order-repo", Substrate: projectmodel.SubstrateGitRepo, Profiles: []string{"local"}, Presence: "required"})
	m.Deployment.Bindings = m.Deployment.Bindings[:1]
	m.Deployment.Bindings[0].PerProfile = map[string]projectmodel.BindingVariant{
		"local": {Variant: "GitLocal", Infra: []string{"order-repo"}},
	}
	src, refusals, err := composegen.EmitScenarioCompose(m, scenarioFixtureCfg)
	if err != nil || len(refusals) != 0 {
		t.Fatalf("EmitScenarioCompose: refusals=%v err=%v", refusals, err)
	}
	if !strings.Contains(string(src), `s.h.Input(s.t, "order-repo", "git-repo", "URL")`) {
		t.Fatalf("the emitted file does not spell the host Input call:\n%s", src)
	}

	platform, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	var gomod strings.Builder
	gomod.WriteString("module example.com/greenfield/server\n\ngo 1.25\n\nrequire (\n" +
		"\t" + platformBase + "framework-go v0.0.0\n" +
		"\t" + platformBase + "framework-go-scenariohost v0.0.0\n)\n\n")
	for _, mod := range []string{"framework-go", "framework-go-scenariohost", "framework-go-projectmodel", "framework-go-app-generator",
		"framework-go-infrastructure-github", "framework-go-infrastructure-postgres", "framework-go-infrastructure-temporal"} {
		gomod.WriteString("replace " + platformBase + mod + " => " + filepath.Join(platform, mod) + "\n")
	}
	var sums []byte
	for _, mod := range []string{"framework-go-app-generator", "framework-go-scenariohost"} {
		raw, readErr := os.ReadFile(filepath.Join(platform, mod, "go.sum"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		sums = append(sums, raw...)
	}
	for rel, body := range map[string]string{
		"go.mod": gomod.String(),
		"go.sum": string(sums),
		"internal/resourceaccess/orderstate/orderstate.go": stubOrderState,
		"internal/scenariocompose/compose.go":              string(src),
	} {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if mkErr := os.MkdirAll(filepath.Dir(full), 0o750); mkErr != nil {
			t.Fatal(mkErr)
		}
		if wErr := os.WriteFile(full, []byte(body), 0o600); wErr != nil {
			t.Fatal(wErr)
		}
	}
	cmd := exec.Command("go", "vet", "./...")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOPROXY=off")
	if out, runErr := cmd.CombinedOutput(); runErr != nil {
		t.Fatalf("the emitted scenariocompose does not build and vet against the real scenario host: %v\n%s", runErr, out)
	}
}
