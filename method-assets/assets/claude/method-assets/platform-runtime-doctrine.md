---
name: platform-runtime-doctrine
kind: doctrine-asset
description: How systems built here run — the platform's fixed, opinionated SDLC runtime doctrine. READ-ONLY, platform-owned, never per-project state. The ten pure-doctrine decisions plus the doctrine halves of the five hybrid decisions, each tiered as a customer one-liner over engineer detail. Referenced (not re-authored) by [[the-method-operational-concepts]]; rendered read-only in the Deployment & Operations Model screen's "How systems built here run" section.
---

# Platform Runtime Doctrine — "How systems built here run"

This is **doctrine**, not a per-project choice. archistrator DECIDES these as its opinionated SDLC: every system built on the platform runs this way, and the customer does not ratify them. This asset is **read-only** and lives with the platform method assets — it is NOT part of any project's `.aiarch/state/project.json`, and it is never commentable or ratifiable in the design experience. The per-project operational choices (deployment scenario, construction venue, review policy, scaling policy, infra building blocks, and the trust summaries) live in the committed `DeploymentOperationsModel` slot instead; see [[the-method-operational-concepts]].

Each entry is tiered: a **customer one-liner** ("how systems built here run") over the **engineer detail**. Doctrine entries carry **no business-objective claim** — they are platform invariants, not per-project decisions justified against a mission.

Because archistrator IS the platform, archistrator's own project references this single asset rather than duplicating the detail — this is the one copy of the engineer-tier doctrine.

---

## Pure doctrine (10)

### 1. Communication topology

**How systems built here run:** requests come in through one gateway and are handled by exactly one service per request; services never chatter behind your back — the one channel between them is a narrow, named message surface, not a free-for-all bus.

**Engineer detail:** External callers reach in-process Clients through Envoy Gateway; Clients call exactly one Manager per request, started or resumed on the durable-execution runtime. **No synchronous HTTP between Managers**, and no broker-style publish/subscribe fan-out. The ONLY inter-Manager channel is the `MessageBus` **utility** — a restricted-clientele utility (ch. 5) exposing exactly two verbs, `deliverSignal` (deliver a queued Manager→Manager message) and `registerSchedule` (register the recurring timers that fire through the SchedulerClient channel). **Only Managers may call it**, enforced by an arch-test import rule; Engines are pure computation, ResourceAccess fronts one resource each, and Clients enter at Managers. It encapsulates the workflow-execution-substrate volatility (Temporal today, another durable executor later) so no Manager binds to a runtime API. A durable-execution runtime is mandatory for every Manager without exception.

### 2. Layering style

**How systems built here run:** the architecture is strictly layered — calls only ever go downward, never up or sideways.

**Engineer detail:** Closed layering per App C §3.4. No Client-to-Engine, Client-to-ResourceAccess, or skip-layer edges. The single permitted sideways call is the **queued Manager→Manager** edge (App C §3.4c.ii) — the book's carve-out for one use case triggering a latent, much-deferred execution of another. Every such edge is declared in the static model, named for the business signal it carries, and realized as that queued call (entry 8): the messaging utility underneath it is never drawn as a second edge for the same delivery.

### 3. Artifact presentation is a client concern

**How systems built here run:** the server hands out structured data; the app draws it — the two never blur.

**Engineer detail:** The server does not render design artifacts. It exposes the typed artifact models as JSON; the React SPA renders them client-side (React-Flow C4 graph, two-axis SVG scatter, React-Flow activity diagrams, react-markdown prose). Every typed model is part of the frozen wire contract via the OpenAPI discriminated `modelEnvelope`.

### 4. Catalog read — one typed read

**How systems built here run:** the whole state of your project is read in a single call, not stitched together from many.

**Engineer detail:** `projectManager.getProject(projectId)` returns the full typed project head-state in one synchronous read (phase, version, research, every artifact slot with kind/stage/model/findings/critique). Thin pass-through over `projectStateAccess.ReadProject`; no per-artifact fan-out, no durable workflow. Powers both the home-base TOC and the design-experience render path.

### 5. Project state storage — git JSON with ref CAS

**How systems built here run:** your project's state is plain files in your own git repo — no database you can't see, and you can always read or take them.

**Engineer detail:** Project state is JSON files in the per-project git repo (behind `projectStateAccess`), mutated by guarded commits under git ref compare-and-swap (push rejected non-fast-forward → reload + retry). Activity-retry idempotency is an in-repo `applied_mutation` record keyed on `workflowId:activityId`. Project state lives in no database — the per-project git repo is its sole store. Operated-system and billing head-state remain Postgres aggregates with version-guarded UPDATE + `applied_mutation` dedup. (The customer-facing consequence — "you own your git repo" — surfaces as the per-project data-ownership trust summary, not here.)

### 6. Implementation language — Go

**Engineer detail (engineer-tier only — no customer one-liner):** Every platform service is written in Go (latest stable). The `unsafe` package is forbidden in platform source via depguard; transitive unsafe use is audited via SBOM + govulncheck. `go test -race` runs on every CI test run. Third-party dependencies only when ubiquitous AND absolutely necessary (launch set: `go.temporal.io/sdk`, `github.com/jackc/pgx/v5`). This is pure technology doctrine and carries no business-objective claim.

### 7. Generated REST clients — Go + TypeScript from one OpenAPI spec

**How systems built here run:** every service's interface is generated from one source of truth, so the clients can never drift from the server.

**Engineer detail:** Every Manager owns one OpenAPI spec. Codegen produces a Go package and a TypeScript npm package per Manager from that spec. The MCP tool surface is a further mechanical projection of the same per-Manager OpenAPI specs (each operation becomes one MCP tool). Both codegen outputs are committed and drift is detected in CI.

### 8. Durable primitives are execution substrate, not architecture edges

**How systems built here run:** the workflow engine is how a service runs, not a component in the design — so the architecture stays clean.

**Engineer detail:** A Manager's workflow-internal durable primitives (`startTimer`, `awaitSignal`, `executeChild`) and each Manager's own `client.Client` are EXECUTION SUBSTRATE, not architecture edges — they are how a Manager's own workflow runs on the durable runtime, not a call from one component to another. The substrate's execution role is therefore **invisible in the architecture**: there is no ResourceAccess and no Resource component standing for the workflow engine, only the `MessageBus` utility (entry 1) carrying the two cross-component verbs. The systemDesign `Manager → MessageBus` edges depict CLIENT-SIDE verb use only (`deliverSignal` / `registerSchedule` against another Manager's workflow), never a Manager's own timers/awaits/child-workflows over its own encapsulated workflow.

**How this shows up in a realization — verb calls draw; deliveries don't.** A queued Manager→Manager relationship is the architectural representation of a delivery, and the bus is the medium of that edge, not a party to it: realize the delivery as the **queued `Manager → Manager` call and never additionally as a `deliverSignal` bus call** — drawing both double-counts one act and re-materializes the substrate this entry makes invisible. A bus call IS drawn where the verb is the activity node's own business work (registering a per-customer recurring schedule on an onboarding chain). Boot-time schedule registration, which no use-case flow performs, is drawn nowhere.

### 9. Resource-access facets — one volatility, one substrate, many contracts

**How systems built here run:** when one store is used several ways, it stays one component with several focused interfaces — never several components pretending to be one store.

**Engineer detail:** A single storage-volatility axis may be fronted by multiple ResourceAccess CONTRACTS only when they are facets of one substrate, each converting one verb family for one Manager audience. Ratified facet groups: Source Control Target → `projectStateAccess` (ONE component, four contract facets: project-state, construction-transition, git-activity-status, design-session) + `sourceControlAccess` + `artifactAccess`; Operational State Store → `operatedSystemStateAccess` + `billingStateAccess` + `usageAccess`. A facet never adds a component, and every facet op must be live — dead ops are pruned at amendment.

### 10. Who is acting is the platform's, not the design's

**How systems built here run:** you never build sign-in into your system — the platform's front door already knows who every request is from, and your system works with that person directly. Run locally, it opens straight onto your own data with no sign-in step.

**Engineer detail:** The platform owns who is acting, not the design.

- **Authentication is the framework edge's.** In the cloud the edge validates the caller's token (the Security utility's token `Validator`, OIDC against the platform identity provider); under the `local` profile the generated edge injects the one dev principal named by `<PREFIX>_DEV_PRINCIPAL`. Every Manager op already receives that caller as `rc.Principal` (`framework-go/manager.Context`), and it travels with the Manager's workflow; the composition root's `PolicyDecisionPoint` (`cmd/server/hooks.go`, checked against `authz.gen.go`) authorizes each (verb, resource) for the principal's own scope.
- **Nothing in the System authenticates or identifies the caller.** Never draw an identity provider as a Resource, never build a ResourceAccess that verifies who the caller is (a credential, a token, a proof, a session), put no field that authenticates or identifies the caller in any contract (no token, proof, session or "actor" material in a request body), and invent no sign-in route, redirect or cookie in a Client or the SPA. "How users authenticate" is already encapsulated by the platform's Security utility and the front door lifted from [[webapp-deployment-prototype]], so it is never a project volatility: deriving it again is speculative encapsulation that duplicates a utility. A customer who names an identity system ("use our corporate Okta") is federated at the front door as deployment configuration, never designed into the app.
- **The identity provider is a deployment node, never in `.systemDesign`.** The prototype names an identity provider because the Deployment & Operations Model draws the front door: that node lives there, and only there. It is not a Resource, and no ResourceAccess fronts it.
- **Ownership stays in the business layers.** Which records belong to whom, and any authorization that depends on business state, is the Manager's (with an Engine where the rule is volatile), keyed on `rc.Principal.Subject`. Do not copy the principal into a workflow input or a request: the Manager already has it.
- **Locally there is no sign-in step.** Opening the app's URL under the `local` profile gives a working app for the dev principal, with nothing for the operator to configure.
- **Credentials that are not the caller's are ordinary.** A credential that is business payload (a product that manages API keys) is a contract field like any other; a credential a ResourceAccess uses to reach an outbound system is a secret in that ResourceAccess's config, not a contract field. If authentication is the customer's business — they are building the identity provider — it goes through ordinary volatility analysis.

---

## Hybrid doctrine halves (5)

Each hybrid decision splits into a **doctrine half** (fixed, below) and a **per-project half** (a typed field in the committed `DeploymentOperationsModel` slot). Only the doctrine half lives here.

### 11. Agentic-job dispatch (doctrine half)

**How systems built here run:** the platform never holds your model keys or your code — it dispatches the work to your own tooling and reads the results back from your git.

**Engineer detail:** Every LLM-needing activity (Phase-1 drafting, Phase-2 plan drafting, Phase-3 construction) is dispatched as an agentic job to the project's configured construction venue, running the customer's own agentic-coding tooling on their own subscription. The platform holds no LLM key, makes no synchronous LLM call, and holds no code text; it orchestrates via `agenticJobAccess` and reads results back from git via `projectStateAccess`. **Per-project half:** the chosen venue → `constructionVenue` (see entry 15).

### 12. Billing model — pricing is a system-wide Strategy

**How systems built here run:** how you're priced is a policy the platform can evolve without re-plumbing anything; the platform bills only for its own service and never touches your end users' money.

**Engineer detail:** Pricing itself is the ServicePricing volatility: `billingEngine` pivots on it as a Strategy (tier floors, free-tier subsidies, and future charge categories such as design/construction work or consulting), so commercial terms evolve without touching the billing workflow. The platform is merchant of record only for its own service invoices; it never touches end-user revenue. ServicePricing is an axis-1 (system-wide) volatility, so the POLICY is platform doctrine. **Per-project half:** the billing trust summary (the customer's plain-language bill basis) is a per-project `TrustSummaries.Billing` field.

### 13. Deployment scenarios are configuration, not architecture

**How systems built here run:** whether the platform just builds your system or also hosts and operates it is a switch — the design is identical either way.

**Engineer detail:** The production scenarios (DEPLOYED — NOT OPERATED; DEPLOYED — OPERATED) differ only by an operation on/off flag — OperationsManager, OperatedRuntime, and hosting billing are simply not instantiated in the not-operated scenario. A test profile swaps the durable runtime to the embedded executor, the git target to an ephemeral repo, and the agentic CI to a local exec runner or stub. The component graph, call chains, and component count are identical across all scenarios. **Per-project half:** the chosen scenario → `deploymentScenario` (`deployedNotOperated` | `deployedOperated`).

### 14. Review-policy gating — configurable oversight with a fixed risk floor

**How systems built here run:** you choose how much human oversight your delivery gets, from fully automatic to a human at every gate — but high-risk changes always get a human, and you cannot turn that off.

**Engineer detail:** Every review gate in the delivery workflow — design-artifact approval and construction change review — is routed by the project's review policy: presets `vibes` (automatic), `checkpoints`, and `full` (human at every gate). A non-overridable risk floor routes high-risk changes to a human under any preset. The gate mechanism is fixed; only the routing policy varies per project. **Per-project half:** the chosen preset → `reviewPolicyRef` (references this fixed preset vocabulary; the risk floor is platform-fixed and not stored per project).

### 15. Construction venue is configuration, not architecture

**How systems built here run:** your system is built on the compute you choose — your own CI or your own machine, on your own account — and switching never changes the design.

**Engineer detail:** A project's drafting and construction jobs run on the venue the customer configures — their own CI on their repository host, or their local machine — behind the same `agenticJobAccess` verbs. Adding or switching a venue never changes a Manager; venue choice is a per-project setting, and today's venues keep construction on the customer's own compute at no charge. **Per-project half:** the chosen venue → `constructionVenue` (`{kind, repositoryHost, note}`).

---

## Book lineage (ch. 5) — two invariants bound platform-wide

Two of the invariants above are Löwy's own ch. 5 operational patterns, bound platform-wide rather than selected per project:

**The durable-execution mandate IS the "workflow Manager" pattern.** Entries 1 and 8's mandatory durable-execution runtime is ch. 5's workflow-Manager operational pattern: the Manager loads a workflow instance — its type, state, and context — executes it, and persists it back, which is exactly what Temporal does for every Manager. The book selects this pattern per project when workflows are long-running, session-free, and device-spanning; the platform binds it for every system.

**The no-internal-message-bus stance is ch. 5's own calibrated fallback.** Entry 1's queued-call topology is not a departure from the book — it is the book's escape hatch: *"In many cases, a simpler design in which the Clients just queue up calls to the Managers would be a better fit… always calibrate the architecture to the capability and maturity… it is a lot easier to morph the architecture than it is to bend the organization."* The platform binds that queued-call pattern (Temporal signals/queues) instead of Message-Is-The-Application.
