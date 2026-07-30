---
name: the-method-operational-concepts
description: System Design — the Deployment & Operations Model. Select the small set of PER-PROJECT operational choices (deployment scenario, construction venue, review policy, scaling policy, infra building blocks), author the three customer trust summaries, and build the deployment-models view. The platform's fixed runtime doctrine (topology, layering, durable primitives, codegen, pricing-as-Strategy) is NOT authored here — it lives read-only in [[platform-runtime-doctrine]] and is referenced. Reads the committed .mission and .systemDesign from project.json. Produces the typed DeploymentOperationsModel committed to project.json → .operationalConcepts (wire kind kept; customer label "Deployment & Operations Model"). Invoke after [[the-method-architecture]], before [[the-method-system-design-standard-check]].
---

# Deployment & Operations Model

The static architecture says what exists. This artifact says how THIS project is deployed and operated — but only the parts the customer actually decides. The re-scope (founder ruling 2026-07-20) split the old "Operational Concepts" slot into two:

1. **Platform runtime doctrine** — how EVERY system built here runs (communication topology, closed layering, durable-primitives-as-substrate, generated clients, git-as-DB, pricing-as-Strategy, the review-preset mechanism, facet doctrine). archistrator DECIDES these; the customer does not ratify them. They live **read-only** in [[platform-runtime-doctrine]]. **You do NOT author them here** — you reference the asset.
2. **Per-project operational choices** — the small ratifiable core THIS customer decides for THEIR project: deployment scenario, construction venue, review policy, scaling policy, infra building blocks, plus three customer trust summaries and the deployment-models view. **That is what this skill produces**, as the typed `DeploymentOperationsModel`.

Every per-project surface is TIERED: a **customer summary** (ratifiable, plain language) over an **engineer detail** (reference). The nine doctrine decisions render ONLY as the read-only "how systems built here run" one-liners from the asset — no engineer tier is surfaced to the customer for those.

## Canonical source

**Primary:** Löwy, Ch. 5 §4.3a "Operational Concepts" — the TradeMe walkthrough (the conceptual origin of this artifact).

**Supporting:**
- Ch. 3 §6 "Open and Closed Architectures" — layering style (now platform doctrine; see the asset)
- Ch. 3 §5 "Subsystems and Services" — subsystem boundaries

**Platform doctrine (referenced, not re-derived):** [[platform-runtime-doctrine]] — the nine pure-doctrine decisions + the doctrine halves of the five hybrids. When you need to state how the runtime works, LINK there; do not restate it as a per-project decision.

## Input

State is git-as-DB: archistrator is a single Go-server repo whose canonical project state lives in `.aiarch/state/project.json` (a typed JSON aggregate). Markdown/DSL is a render-on-read of the typed state.

- The committed **mission** artifact → `.mission` — every PER-PROJECT field links to a **stable objective id** (the objective's declared `number` in the committed mission, not a list position), written into the typed `objectiveLinks` map. Doctrine content carries no objective claim.
- The committed **systemDesign** artifact → `.systemDesign` (the typed `System`)
- [[platform-runtime-doctrine]] — the read-only doctrine asset you reference (never copy into the slot)

## Output

The typed **`DeploymentOperationsModel`** (Go shape in `internal/resourceaccess/projectstate/models_phase1.go`), committed to **`.aiarch/state/project.json` → `.operationalConcepts`** (the wire kind keeps its identity; the customer-facing label is "Deployment & Operations Model"). NOT a `*.md` file — any markdown below is a render-on-read of this slot. The old `{ decisions[], deployment }` shape is gone; there is no free-form `decisions[]` array. The model carries:

- `objectiveLinks` — map: per-project knob name → array of objective numbers from the committed `.mission` (the typed home of every per-project objective link; see Step 1)
- `deploymentScenario` — `deployedNotOperated` | `deployedOperated`
- `constructionVenue` — `{ kind (customerCI | localMachine), repositoryHost, note }`
- `reviewPolicyRef` — `vibes` | `checkpoints` | `full` (a reference to the platform preset vocabulary; the risk floor is platform-fixed and not stored here)
- `scalingPolicy` — `{ scaleToZero, minInstances, maxInstances, targetUtilization }` (only when `deploymentScenario == deployedOperated`)
- `infraBuildingBlocks` — the supported building blocks THIS app uses (`{ name, category, status }`)
- `trustSummaries` — `{ billing, usageMetering, dataOwnership }`
- `deploymentModel` — the deployment-models view (existing `DeploymentView`/topology shape, retained)

## Procedure

The architect owns this entirely. No PM input needed for the mechanics; the PM ratifies the customer-summary tier.

### Step 1 — Select the per-project fields (link each to a stable objective id)

For each field below, choose the value for THIS project and link it to the **stable objective id** it serves. Reference the doctrine asset for the fixed half — do not re-author it.

**Write every link into the typed `objectiveLinks` field** — a map from knob name to an array of objective numbers from the committed `.mission`. The "Typical objective link" column below is guidance; the committed link lives in `objectiveLinks`. Every number must resolve to a declared objective (`DH-OBJ-RESOLVE`, Error); an objective referenced by no knob surfaces as `DH-OBJ-COVERAGE` (Warning).

| Field | Choose | Doctrine half (reference) | Typical objective link |
|---|---|---|---|
| `deploymentScenario` | build-and-hand-off vs also-host-and-operate | [[platform-runtime-doctrine]] #12 | revenue-flexibility objective |
| `constructionVenue` | the customer's CI on their repo host, or their local machine | [[platform-runtime-doctrine]] #10, #14 | revenue-flexibility objective |
| `reviewPolicyRef` | `vibes` / `checkpoints` / `full` | [[platform-runtime-doctrine]] #13 | configurable-SDLC objective |
| `scalingPolicy` | scale-to-zero + min/max/target (operated only) | [[platform-runtime-doctrine]] #12 | we-handle-the-hard-parts / operations objective |
| `infraBuildingBlocks` | the supported blocks this app uses, from the platform-permitted set | [[platform-runtime-doctrine]] #9 (facet/Strategy) | non-coder-accessibility objective |

`scalingPolicy` and the operate/bill infra are ABSENT when `deploymentScenario == deployedNotOperated` — the slot tolerates their absence; do not invent them for a build-and-hand-off project.

### Step 2 — Author the three customer trust summaries

Plain-language, ratifiable prose the non-engineer customer reads as a promise. Each has an engineer-tier detail underneath, but the summary is what is ratified.

- `trustSummaries.billing` — e.g. "We meter only what your running app consumes; that's the sole basis of your bill." (doctrine half: [[platform-runtime-doctrine]] #11).
- `trustSummaries.usageMetering` — the Usage Log story: "The append-only record of exactly what your running app consumed — the only thing you are billed on, and readable by you at any time." The `CloudNativePG · Postgres 16` technology is engineer-tier ONLY.
- `trustSummaries.dataOwnership` — "Your entire project — state and source — lives in your own git repo; export or leave any time." (doctrine half: [[platform-runtime-doctrine]] #5).

### Step 3 — Build the deployment-models view (`deploymentModel`)

The deployment view is the deployment volatility made visible. Author it in the existing `DeploymentView`/topology shape, with these corrections (founder ruling §5):

- **OperatedRuntime is NOT an external service.** It is the PRODUCT the platform hosts. In the `deployedOperated`/cloud environment it is a first-class workload node INSIDE the managed cluster (the operated-tenant namespace — the `gtd namespace` is exactly "example operated-system namespace"; label it "example"). Present ONLY in `deployedOperated`. External services keep only genuine third parties: GitHub, Stripe/MerchantGateway, Keycloak, the customer's ProjectGitRepo host.
- **The LOCAL profile tells the real local story** (read from `server/cmd/archistrator/init.go` + `serve.go`): the `archistrator-server` child process (HTTP API + `/mcp` mount + embedded SPA on `127.0.0.1:8877`), the embedded Temporal dev-server (the local DurableExecutionRuntime), the on-disk `.aiarch` git repo (the local ProjectGitRepo; first design session seeds `project.json` + the default `vibes` preset), and the local agentic runner (`claude -p` on the user's own subscription). Show the explicitly-absent pieces (no Postgres, no Keycloak, no GitHub App, no platform-held LLM key) as a "not present in local" annotation — never a blank box.
- **Per-environment resources are env-accurate and scenario-gated.** cloud: OperatedSystemState / BillingState / UsageLog on the CloudNativePG cluster; ProjectGitRepo on the customer's GitHub. test: ephemeral stubs. local: ProjectGitRepo = on-disk `.aiarch` git ONLY — the operate/bill stores are ABSENT (local designs + constructs, it does not operate or bill). The operate/bill stores render only under `deployedOperated`; the view code reads `deploymentScenario`, it does not hardcode three tabs.

### Step 4 — Tier every per-project surface (customer summary over engineer detail)

Every field from Steps 1–3 renders as a **customer summary (ratifiable)** with the **engineer detail (reference, collapsed)** beneath. Examples:

| Per-project item | Customer summary (ratifiable) | Engineer detail |
|---|---|---|
| `deploymentScenario` | "We build & hand off your system" / "We also host & operate it for you." | scenario flag semantics; which components instantiate |
| `constructionVenue` | "Your code is built on <your CI / your machine>, on your own account." | `agenticJobAccess` verbs; venue mechanics |
| `reviewPolicyRef` | "Oversight: <vibes = fully automatic / checkpoints / human at every gate>. High-risk changes always get a human." | preset routing table; risk-floor rule |
| `scalingPolicy` | "We scale your app to demand, down to zero when idle." | AutoscalerEngine tunables |
| `infraBuildingBlocks` | "Your app is built from these supported building blocks: …" | permitted-set membership, Strategy registrations |
| `trustSummaries.*` | the billing / usage / data-ownership one-liners above | Postgres/git tech, ref-CAS, metering mechanics |

The nine doctrine decisions render ONLY as the read-only "how systems built here run" one-liners from [[platform-runtime-doctrine]] — no engineer tier is surfaced to the customer for them. For archistrator's OWN project (dogfooding), the full engineer-tier doctrine detail lives in the platform asset archistrator authors — one copy, referenced, not duplicated into the slot.

### Step 5 — Cross-checks

| Check | Action |
|---|---|
| Every PER-PROJECT field links to a stable objective id in the typed `objectiveLinks` map (knob → objective numbers; `DH-OBJ-RESOLVE` Error on a dangling number, `DH-OBJ-COVERAGE` Warning on an unreferenced objective) | Write the link into `objectiveLinks` or drop the field |
| No doctrine decision is re-authored in the slot | Move it out — reference [[platform-runtime-doctrine]] instead |
| Doctrine content carries NO objective claim | Strip any objective link that crept onto a doctrine reference |
| `scalingPolicy` / operate-bill infra absent when `deployedNotOperated` | Remove them; the slot tolerates absence |
| Deployment view: OperatedRuntime inside the cluster (not external), local story real, resources scenario-gated | Fix the view |
| Every per-project surface has a customer summary over engineer detail | Add the tier |

## Draft-job doctrine (CI dispatch)

This is the normative task the CI draft job (and a local `/system-design` run) executes to produce the `DeploymentOperationsModel`. It is self-contained.

Do NOT author the platform runtime doctrine (topology, layering, durable primitives, codegen, pricing-as-Strategy, the review-preset mechanism, facet doctrine) — that is fixed platform doctrine and lives read-only in [[platform-runtime-doctrine]]; REFERENCE it. Author ONLY the per-project operational choices:

- Select `deploymentScenario` (`deployedNotOperated` | `deployedOperated`), `constructionVenue` (`{kind, repositoryHost, note}`), `reviewPolicyRef` (`vibes` | `checkpoints` | `full`, a reference to the platform preset vocabulary — the risk floor is platform-fixed, not stored), `scalingPolicy` (only under `deployedOperated`), and `infraBuildingBlocks` (from the platform-permitted set). WRITE each per-project field's objective link into the typed `objectiveLinks` map (knob name → array of objective numbers from the committed `.mission`): every number must resolve to a declared objective (`DH-OBJ-RESOLVE`, Error) and an objective referenced by no knob surfaces as `DH-OBJ-COVERAGE` (Warning). Doctrine content carries NO objective claim and never appears in `objectiveLinks`.
- Author the three `trustSummaries` (billing / usageMetering / dataOwnership) as plain-language customer promises.
- Every per-project surface is TIERED: a ratifiable customer summary over an engineer-tier detail.

Then populate the deployment-models view (`deploymentModel`) in C4-container shape. The set of deployment environments is DERIVED from `deploymentScenario` and a test profile is ALWAYS present. Declare the top-level `containers` array — the deployable UNITS, not the components — each with a `key`, `name`, `technology`, `description`, and `components` listing the exact NAMES of the System components it packages. Every CODE component — every Client, Manager, Engine, and ResourceAccess, plus every Utility — MUST be packaged into EXACTLY ONE container. Resources are NOT container members — model each as an infrastructureNode (a self-describing name/technology/description) or, for a genuinely external third party, as a softwareSystemInstance. Each environment nests deploymentNodes whose containerInstances reference a declared container BY ITS `containerKey` and set an `instances` integer. CROSS-PROFILE INVARIANT: operating mode is configuration, not architecture — the set of deployed CONTAINERS MUST be IDENTICAL across environments (the underlying infrastructure MAY legitimately differ per profile). Apply the §5 view corrections: OperatedRuntime is a workload node INSIDE the operated cluster (present only under `deployedOperated`), NOT an external service; the LOCAL profile shows the real local story (archistrator-server child process, embedded Temporal, on-disk `.aiarch` git, local `claude -p` runner) with an explicit "not present in local" annotation for the absent Postgres/Keycloak/GitHub-App/LLM-key; per-environment resources are env-accurate and scenario-gated (operate/bill stores appear only under `deployedOperated`). Reference containers by `containerKey` and System components by NAME.

### Operating-model deployment constraint

`deploymentScenario` constrains the deployment topology the view may model:

**`deployedNotOperated` (build & hand off).** The customer runs the built app in their OWN infrastructure, so the OPEN deployment guidance stands — no extra constraint. The operate/bill stores and `scalingPolicy` are ABSENT.

**`deployedOperated` (platform-hosted & operated).** The deployment is CONSTRAINED to the archistrator-platform infrastructure ONLY. Model the deployment using EXACTLY these platform building blocks; do NOT introduce bespoke or third-party cloud infrastructure:

- Data / persistence: CloudNativePG (CNPG) Postgres — the framework-go-infrastructure-postgres module. Model every relational Resource as a CNPG Postgres cluster infrastructureNode.
- Workflows / durable execution: Temporal — the framework-go-infrastructure-temporal module (the SHARED platform Temporal at software/k8s/shared/temporal).
- Authentication / identity: Keycloak — the framework-go-infrastructure-keycloak module.
- Observability: the OpenTelemetry stack — the framework-go-infrastructure-otel module.
- Deploy target: the platform Kubernetes cluster via the ArgoCD stack at software/k8s; every container is a Kubernetes Deployment in the platform cluster and every infrastructureNode names the exact framework-go-infrastructure-* module above.

FORBIDDEN for `deployedOperated`: AWS (RDS, EKS, ECS, CloudFront, S3, Lambda), GCP, Azure, or any other bespoke / self-managed / third-party-managed cloud infrastructure — those are legitimate ONLY for `deployedNotOperated` projects the customer runs themselves.

## Exit criteria (for router)

`.aiarch/state/project.json` → `.operationalConcepts` holds the typed `DeploymentOperationsModel`: the per-project fields chosen (each linked to a stable objective id via the typed `objectiveLinks` map), the three trust summaries authored, and the deployment-models view built with the §5 corrections. No platform-doctrine decision is authored in the slot — doctrine is referenced from [[platform-runtime-doctrine]]. Every per-project surface is tiered (customer summary over engineer detail).

Move to `the-method-system-design-standard-check`.

## Anti-patterns to reject

- **Re-authoring platform doctrine in the slot** — topology, layering, durable-primitives, codegen, pricing-as-Strategy, review-preset mechanism, facet doctrine are FIXED and live in [[platform-runtime-doctrine]]; reference them, never re-decide them per project.
- **An objective claim on doctrine content** — doctrine carries no business-objective link; that was the rubber-stamp defect. Only per-project fields link to objectives.
- **Position-based objective links** — `objectiveLinks` values are the objectives' declared `number`s in the committed `.mission` (their stable ids), never list positions; a dangling number is `DH-OBJ-RESOLVE` (Error).
- **OperatedRuntime as an external service** — category error; it is the product, a workload node inside the operated cluster, present only under `deployedOperated`.
- **An empty "developer laptop" box for local** — tell the real local story; annotate the absent pieces.
- **Postgres/operate-bill boxes in a `deployedNotOperated` or local view** — those stores exist only under `deployedOperated`; scenario-gate them.
- **A per-project surface with no customer summary** — every ratifiable field needs the plain-language tier over its engineer detail.
