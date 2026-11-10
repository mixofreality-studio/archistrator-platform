// Package rungen emits an app's local run contract, cmd/devstack/main.gen.go, from the same
// effective deployment model configgen and composegen read (FIX-049). The launcher is a
// rendering of the deployment model, not a component: it is no Method box and never ships in
// the container. In order it
//
//  1. checks its prerequisites (go, and the temporal CLI, docker and npm when the local
//     profile needs them) and fails naming any that is missing;
//  2. provisions the substrates it knows for the local profile: a Temporal dev server
//     (temporal server start-dev), a postgres:16-alpine container and a temporary bare git
//     repo per git-repo infrastructure key;
//  3. runs the managed web build when the System has a browser surface;
//  4. sets the <PREFIX>_* variables, named by configgen.Inputs, and boots cmd/server;
//  5. waits for /readyz and prints one machine-readable line with the URL.
//
// A required local input it cannot provision (an external HTTP endpoint, a GitHub App, an
// IdP) is required from the caller's environment, and the launcher fails naming it: it never
// invents a double for an external system. Every app gets the same launcher shape from its
// own model, so nothing in it knows any one app.
package rungen

import (
	"fmt"
	"go/format"
	"slices"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/configgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// Dir is the launcher's package directory, module-root-relative; File is its one file.
const (
	Dir  = "cmd/devstack"
	File = "main.gen.go"
)

// defaultWebDir is the web project every browser surface shares (webgen's bootstrap seats it).
const defaultWebDir = "webApp"

// Config parameterizes Generate.
type Config struct {
	// ContainerKey is the deployment container the server is composed for.
	ContainerKey string
	// EnvPrefix is the env-var namespace, as configgen's.
	EnvPrefix string
	// WebDir is the web project directory, module-root-relative (default "webApp"). It is
	// built only when the System has a browser surface.
	WebDir string
}

// Provision is how a variable the launcher sets gets its value.
type Provision string

// The values the launcher provisions.
const (
	TemporalHostport  Provision = "temporal.hostport"
	TemporalNamespace Provision = "temporal.namespace"
	PostgresURL       Provision = "postgres.url"
)

// GitRepoURL is the provision of the URL of the temporary bare repo made for git-repo
// infrastructure key.
func GitRepoURL(key string) Provision { return Provision("git-repo." + key + ".url") }

// Plan is the local run contract derived from the deployment model.
type Plan struct {
	Temporal, Postgres bool
	// GitRepos is the git-repo infrastructure keys the launcher makes a temporary bare repo for,
	// sorted.
	GitRepos []string
	// WebDir is the web project to build ("" when there is no browser surface to serve).
	WebDir string
	// ListenEnv, WebDistEnv and DevPrincipalEnv are the variables the launcher sets itself.
	ListenEnv, WebDistEnv, DevPrincipalEnv string
	// Provisioned maps each variable the launcher sets from what it provisioned.
	Provisioned []ProvisionedEnv
	// Required is the required local inputs the launcher cannot provision, required from the
	// caller's environment.
	Required []string
}

// ProvisionedEnv is one variable set from a provisioned substrate.
type ProvisionedEnv struct {
	Env  string
	From Provision
}

// Generate emits {"main.gen.go": src}: the launcher for m's local profile.
func Generate(m *projectmodel.Model, cfg Config) (map[string][]byte, error) {
	p, err := Derive(m, cfg)
	if err != nil {
		return nil, err
	}
	src, err := format.Source([]byte(render(p)))
	if err != nil {
		return nil, fmt.Errorf("rungen: gofmt: %w", err)
	}
	return map[string][]byte{File: src}, nil
}

// Derive is the local run contract for m: every input the generated Config requires under the
// local profile is either provisioned or required from the caller, and every variable the
// launcher sets is one the Config reads.
func Derive(m *projectmodel.Model, cfg Config) (Plan, error) {
	inputs, err := configgen.Inputs(m, configgen.Config{ContainerKey: cfg.ContainerKey, EnvPrefix: cfg.EnvPrefix, PackageName: "main"})
	if err != nil {
		return Plan{}, fmt.Errorf("rungen: %w", err)
	}
	var p Plan
	devPrincipal := configgen.DevPrincipalEnv(cfg.EnvPrefix)
	for _, in := range inputs {
		if in.Env == devPrincipal {
			p.DevPrincipalEnv = in.Env
			continue
		}
		// The framework's own local input, set by a caller that launches the app with several
		// principals (scenariohost.LaunchDevstack): neither provisioned nor required.
		if in.Env == configgen.DevPrincipalsEnv(cfg.EnvPrefix) {
			continue
		}
		if err := p.take(in); err != nil {
			return Plan{}, err
		}
	}
	if p.ListenEnv == "" {
		return Plan{}, fmt.Errorf("rungen: the deployment declares no %q setting, so the server has no HTTP address to run on", "listenAddr")
	}
	if err := p.web(m.System.HasBrowserSurface(), cfg.WebDir); err != nil {
		return Plan{}, err
	}
	return p, nil
}

// web builds and serves the web project only when the System has a browser surface.
func (p *Plan) web(hasSurface bool, dir string) error {
	if !hasSurface {
		p.WebDistEnv = ""
		return nil
	}
	if p.WebDistEnv == "" {
		return fmt.Errorf("rungen: the System has a browser surface but the deployment declares no %q setting to serve it by", projectmodel.WebDistDirSetting)
	}
	p.WebDir = dir
	if p.WebDir == "" {
		p.WebDir = defaultWebDir
	}
	return nil
}

// take files one Config input into the plan.
func (p *Plan) take(in configgen.Input) error {
	switch {
	case in.Setting == "listenAddr":
		p.ListenEnv = in.Env
	case in.Setting == projectmodel.WebDistDirSetting:
		p.WebDistEnv = in.Env
	case in.InfraKey == "" || !slices.Contains(in.Profiles, projectmodel.LocalProfile):
		// A setting with a default, or infrastructure the local profile does not use.
	default:
		return p.takeInfra(in)
	}
	return nil
}

// takeInfra files one local-profile infrastructure input: provisioned when the launcher can make
// the substrate, otherwise required of the caller when the Config requires it.
func (p *Plan) takeInfra(in configgen.Input) error {
	switch in.Substrate {
	case projectmodel.SubstrateTemporal:
		p.Temporal = true
		return p.provision(in, map[string]Provision{"HOSTPORT": TemporalHostport, "NAMESPACE": TemporalNamespace})
	case projectmodel.SubstratePostgres:
		p.Postgres = true
		return p.provision(in, map[string]Provision{"URL": PostgresURL})
	case projectmodel.SubstrateGitRepo:
		if !slices.Contains(p.GitRepos, in.InfraKey) {
			p.GitRepos = append(p.GitRepos, in.InfraKey)
			slices.Sort(p.GitRepos)
		}
		return p.provision(in, map[string]Provision{"URL": GitRepoURL(in.InfraKey)})
	case projectmodel.SubstrateGitHubApp, projectmodel.SubstrateKeycloak, projectmodel.SubstrateOtel, projectmodel.SubstrateExternalHTTP:
		if in.RequiredFor(projectmodel.LocalProfile) {
			p.Required = append(p.Required, in.Env)
		}
	}
	return nil
}

func (p *Plan) provision(in configgen.Input, by map[string]Provision) error {
	from, ok := by[in.CatalogInput]
	if !ok {
		return fmt.Errorf("rungen: %s substrate input %s: the launcher does not know how to provision it", in.Substrate, in.CatalogInput)
	}
	p.Provisioned = append(p.Provisioned, ProvisionedEnv{Env: in.Env, From: from})
	return nil
}

// Names is every variable the launcher sets or requires.
func (p Plan) Names() []string {
	names := []string{p.ListenEnv, p.DevPrincipalEnv}
	if p.WebDistEnv != "" {
		names = append(names, p.WebDistEnv)
	}
	for _, e := range p.Provisioned {
		names = append(names, e.Env)
	}
	return append(names, p.Required...)
}

// render is the launcher's source: the plan, then the fixed runtime.
func render(p Plan) string {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("// plan is the local profile's run contract, rendered from the deployment model.\n")
	b.WriteString("var plan = runPlan{\n")
	fmt.Fprintf(&b, "\tTemporal: %t,\n\tPostgres: %t,\n", p.Temporal, p.Postgres)
	b.WriteString("\tGitRepos: []string{")
	for i, k := range p.GitRepos {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%q", k)
	}
	b.WriteString("},\n")
	fmt.Fprintf(&b, "\tWebDir: %q,\n\tListenEnv: %q,\n\tWebDistEnv: %q,\n\tDevPrincipalEnv: %q,\n", p.WebDir, p.ListenEnv, p.WebDistEnv, p.DevPrincipalEnv)
	b.WriteString("\tProvisioned: []provisionedEnv{\n")
	for _, e := range p.Provisioned {
		fmt.Fprintf(&b, "\t\t{Env: %q, From: %q},\n", e.Env, string(e.From))
	}
	b.WriteString("\t},\n\tRequired: []string{")
	for i, r := range p.Required {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%q", r)
	}
	b.WriteString("},\n}\n")
	b.WriteString(runtime)
	return b.String()
}
