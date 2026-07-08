package modelgen

// layerContext maps a Method layer to the per-layer call Context the generator
// prepends to every interface method (aliased import + Go type).
var layerContext = map[string]struct{ alias, path, typ string }{
	"engine":         {"fweng", "github.com/mixofreality-studio/archistrator-platform/framework-go/engine", "fweng.Context"},
	"resourceaccess": {"fwra", "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess", "fwra.Context"},
	"manager":        {"fwm", "github.com/mixofreality-studio/archistrator-platform/framework-go/manager", "fwm.Context"},
	"client":         {"fwc", "github.com/mixofreality-studio/archistrator-platform/framework-go/client", "fwc.Context"},
}

// infraField is one constructor parameter / struct field an infra binding
// contributes to the generated <Infra><Component> struct + constructor: the field
// name, its Go type, and the imports that type needs (path -> alias, "" = none).
type infraField struct {
	name    string
	typ     string
	imports map[string]string
}

// infraBinding is one APPROVED framework infra's generated-impl shape. Two modes:
//
//   - ARTIFACT mode (delegated=false, the Stage-1 thin-wrapper): modelgen emits a
//     concrete <Infra><Component> struct holding the framework client field(s) (the
//     params), a public New<Infra><Component>(params) <Iface> constructor, and a
//     compile-time assertion. The interface methods are hand-written on the
//     generated (exported) struct. Used by artifact (Git).
//   - OPTION-1 DELEGATED mode (delegated=true, the stateful RAs): modelgen emits ONLY
//     the public delegating constructor
//     New<Infra><Component>(params) (<Iface>[, error]) whose body is
//     `return new<Infra><Component>(args)`. The impl struct, the unexported builder
//     new<Infra><Component>, and the interface methods are ALL hand-written +
//     unexported in the RA package (so the concrete impl + its package-local state —
//     token caches, a kindRegistry, DDL-applying constructors, idempotency stores,
//     seam interfaces — stay unexported; only the generated interface + models + the
//     constructor are public). No generated struct, no generated assertion (the
//     hand-written builder returning the interface is the compile-time proof).
//
// returnsError selects the constructor's return signature: (<Iface>, error) when the
// hand-written builder can fail (validation / DDL), or just <Iface> when construction
// is infallible. params are the constructor parameters (the framework client(s) +
// any ctx/extra the hand-written builder needs); their types are the REAL framework /
// package-local Go types the current hand-written impls are built on.
type infraBinding struct {
	delegated    bool
	returnsError bool
	params       []infraField
}

// infraBindings maps an APPROVED framework infra name to the field(s) its client
// — plus any per-call dependency the satellite's API forces — contributes to the
// generated <Infra><Component> struct + constructor. The client Go types are the
// REAL framework-go-infrastructure-<infra> types the current hand-written impls are
// built on. An infra absent from this map is rejected (only approved framework
// infra may appear on a generated impl).
//
// Git: the artifact RA is built on framework-go-infrastructure-github's
// *GitBlobStore (gitstore.go's NewLocalStore/NewCloudStore both wrap
// fwgithub.NewGitBlobStore). That satellite's blob ops take a per-call
// fwgithub.GitAuth, resolved differently per deployment profile (LOCAL =>
// GitAuth{Local:true}; CLOUD => an internally-minted installation token), so the
// Git binding contributes a SECOND field — an auth resolver func — alongside the
// bare blob client. The composition root supplies both (the profile-specific auth
// resolver lives there); this is the documented deviation from a pure
// (module, client-type) mapping, forced by the satellite's per-call-auth surface.
var infraBindings = map[string]infraBinding{
	// artifact (Stage-1 thin wrapper): generated exported struct + constructor.
	"Git": {
		params: []infraField{
			{
				name:    "git",
				typ:     "*fwgithub.GitBlobStore",
				imports: map[string]string{"github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github": "fwgithub"},
			},
			{
				name: "auth",
				typ:  "func(ctx context.Context) (fwgithub.GitAuth, error)",
				imports: map[string]string{
					"context": "",
					"github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github": "fwgithub",
				},
			},
		},
	},

	// --- OPTION-1 DELEGATED RAs (stateful; impl hand-written + unexported) -------

	// sourcecontrol → GitHub. The hand-written builder wires the unexported githubClient
	// seam over the framework *fwgithub.AppClient (which satisfies the seam directly) plus
	// the composition-root config (default account / App slug / repo visibility), then the
	// access impl (holding the token cache + clock seam). Validation can fail (returnsError).
	"GitHub": {
		delegated:    true,
		returnsError: true,
		params: []infraField{
			{name: "client", typ: "*fwgithub.AppClient", imports: map[string]string{"github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github": "fwgithub"}},
			{name: "defaultAccount", typ: "string"},
			{name: "appSlug", typ: "string"},
			{name: "repoPrivate", typ: "bool"},
		},
	},

	// usage → Postgres. The hand-written builder applies the embedded schema.sql
	// DDL, so construction can fail (returnsError). Param types are the current
	// NewPostgresUsageAccess(ctx, pool) shape.
	"Postgres": {
		delegated:    true,
		returnsError: true,
		params: []infraField{
			{name: "ctx", typ: "context.Context", imports: map[string]string{"context": ""}},
			{name: "pool", typ: "*pgxpool.Pool", imports: map[string]string{"github.com/jackc/pgx/v5/pgxpool": ""}},
		},
	},

	// durableexecution → Temporal. framework-go-infrastructure-temporal has NO client
	// wrapper, so the constructor takes the RAW go.temporal.io/sdk/client.Client the
	// impl uses (the documented exception — no framework type exists). The kind→binding
	// table builds the hand-written kindRegistry. Construction is infallible.
	"Temporal": {
		delegated:    true,
		returnsError: false,
		params: []infraField{
			{name: "cl", typ: "client.Client", imports: map[string]string{"go.temporal.io/sdk/client": ""}},
			{name: "table", typ: "map[ExecutionKind]KindBinding"},
		},
	},

	// constructionpipeline → GitHubActions. The hand-written builder wires the
	// ghActionsClient seam (token-caching ghActionsRESTClient) over the framework
	// *fwgithub.AppClient + the repo/workflow config, then the Access impl. Validation
	// can fail (returnsError).
	"GitHubActions": {
		delegated:    true,
		returnsError: true,
		params: []infraField{
			{name: "app", typ: "*fwgithub.AppClient", imports: map[string]string{"github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github": "fwgithub"}},
			{name: "owner", typ: "string"},
			{name: "repo", typ: "string"},
			{name: "workflowFile", typ: "string"},
			{name: "ref", typ: "string"},
			{name: "installationID", typ: "int64"},
		},
	},

	// worker → per-client constructors (NOT one "LLM"). Each delegates to its
	// hand-written unexported impl (which holds its *idemStore + config).
	"Anthropic": {
		delegated:    true,
		returnsError: true,
		params: []infraField{
			{name: "client", typ: "*fwllm.AnthropicClient", imports: map[string]string{"github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-llm": "fwllm"}},
			{name: "defaultModel", typ: "string"},
			{name: "classModels", typ: "map[WorkerClass]string"},
		},
	},
	"Ollama": {
		delegated:    true,
		returnsError: true,
		params: []infraField{
			{name: "client", typ: "*fwllm.Client", imports: map[string]string{"github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-llm": "fwllm"}},
			{name: "defaultModel", typ: "string"},
			{name: "classModels", typ: "map[WorkerClass]string"},
		},
	},
	// Replay decorates an optional real WorkerAccess delegate with on-disk cassettes;
	// mode is the package-local ReplayMode param type, delegate the contract interface.
	"Replay": {
		delegated:    true,
		returnsError: true,
		params: []infraField{
			{name: "dir", typ: "string"},
			{name: "mode", typ: "ReplayMode"},
			{name: "delegate", typ: "WorkerAccess"},
		},
	},

	// operatedRuntime → Profiled. The tenant runtime (GitOps/kubernetes + observability)
	// has no framework infrastructure client yet, so — like Replay — the binding takes
	// package-local param types (RuntimeProfile selector + RuntimeConfig) rather than a
	// framework client. The hand-written builder selects the deterministic LOCAL/dry-run
	// impl (no backing infra) or the REAL impl skeleton (fails fast / surfaces an explicit
	// Unavailable naming the kubernetes follow-up until that backend lands). Construction
	// can fail (real-profile config validation), so returnsError.
	"Profiled": {
		delegated:    true,
		returnsError: true,
		params: []infraField{
			{name: "profile", typ: "RuntimeProfile"},
			{name: "config", typ: "RuntimeConfig"},
		},
	},
}
