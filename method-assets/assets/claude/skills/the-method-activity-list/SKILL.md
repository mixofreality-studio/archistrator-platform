---
name: the-method-activity-list
description: Project Design — produce the activity list (coding + noncoding) with 5-day quantum estimates. ONE coding activity per component (detailed-design and construction are internal lifecycle phases, not separate activities), plus integration and noncoding. Reads the committed systemDesign and planningAssumptions artifacts in project.json. Produces the typed ActivityList committed to project.json → .activityList. Invoke after [[the-method-planning-assumptions]], before [[the-method-network-draft]].
---

# Activity List

The architecture defines what to build. The activity list says how the work decomposes into estimable units. Every activity is in 5-day quanta, ≤35 days, with role assignment and behavioral dependencies.

## Canonical source

**Primary:**
- Löwy, Ch. 7 §5 "Effort Estimations" — estimation rules
- Ch. 7 §5.3 "Activity Estimations"
- Ch. 11 §1.2a "List of Activities" — first worked example
- Ch. 13 §1.1 "Individual Activity Estimations" — second worked example

**Noncoding activities reference:** Ch. 13 — Table 13-3 shows the full noncoding activity inventory from TradeMe.

**Standard reference:** Appendix C §4.4 "Estimations" — quantum of 5 days, no god activities, accuracy over precision.

## Input

State is git-as-DB: all of this lives in `.aiarch/state/project.json` (a typed JSON aggregate), NOT in `designs/<product>/*.md` files. Markdown/DSL is a render-on-read of the typed state, never the source of truth.

- The committed **systemDesign** artifact in `project.json` → `.systemDesign` — the architecture decomposition: each component → coding activities; relationships → integration activities. (When rendered as Structurizr DSL, each component is a `container`.)
- The committed **planningAssumptions** artifact in `project.json` → `.planningAssumptions`

## Output

**`.activityList` holds a delta document, not a materialized list.** The baseline activity set — every `C-*` coding activity, `R-*` resource provisioning, `U-SPA-<manager>` and `U-SPA-S` SPA construction, `G-SPA` UI-design concept, and the always-emit `N-*` testing/QA inventory — is **derived mechanically** by `estimationEngine.DerivePlan` from the committed `systemDesign` artifact, plus the dependency network and milestones that follow from the System's relationships. It is a **render-on-read**: re-running `DerivePlan` against the same committed System reproduces it exactly, and `make derived-plan-check` fails the build if it doesn't.

What the agent authors into `.activityList` is the typed `ActivityListDeltas` model — the entire human-review surface, and nothing else:
- `overrides` — an `ActivityOverride` per derived activity whose computed `effortDays` or `riskBucket` is wrong for this project, each carrying a written `justification`.
- `additive` — an `AdditiveActivity` for work that maps to no single component (environment setup, security review, documentation, training, deployment, …), each with its own incident edges and a written `justification`. Additive activities are **genuinely componentless** — one that names a `componentId` is rejected, because a component-bound additive is a covert exclusion/replacement channel for a derivation the architecture should instead fix.
- `additiveMilestones` — additional milestones that depend on additive activities (e.g. a "v1 Production Live" milestone cannot derive, since it depends entirely on additive noncoding work).

Two usage patterns produce this slot:

1. **Agentic/CI dispatch:** the agent produces the typed `ActivityListDeltas` model as JSON and commits it into `.activityList` on its session branch; the server applies the deltas onto the derived baseline (`DerivePlan`) and stages the result (`StageArtifactForReview`) for the human review gate (`CommitArtifact` / `RejectArtifact`).
2. **Local interactive:** same — produce the typed deltas model and write it into the `.activityList` slot. Never a `designs/*.md` file, and never a hand-typed materialized activity list.

## Worker classes and the activity inventory — canonical doctrine

Two normative rules govern this activity list: every activity's `workerClass` is drawn from the fixed Method team roster (and must have a `rateCard` entry in the committed PlanningAssumptions), and each activity is named with its short network id under the fixed prefix conventions — with a component whose work is not yours to plan (generated transport, or a platform/third-party-provided component) getting NO coding activity, and the standard UI/testing inventory ALWAYS derived. The canonical statements of both rules live under **Draft-job doctrine → Worker classes are a fixed roster** and **→ What derives (the closed baseline)** below; they apply identically here. Where the book says UX-designer or DevOps, use `ui-designer` and `senior-developer`.

## Procedure

### Step 1 — Coding activities per component

**One activity per component.** For each component declared in the committed `systemDesign` artifact (every component is a `container` in the rendered DSL), emit **exactly one** coding activity. Detailed design and construction are **internal phases of that activity's lifecycle** (App A — every activity "is its own little life cycle" with Requirements → Detailed Design → Test Plan → Construction → Integration phases), dispatched to different roles per phase (senior-developer designs the contract in the detailed-design phase; junior-developer builds in the construction phase per [[the-method-handoff]]). They are **not** two separate activities.

> This is the deliberate correction of the "clock": do NOT emit a `D###` design activity *and* a `C###` construction activity per component. The base activity list is one activity per component; the per-phase role hand-off lives inside the lifecycle. Pulling contract design out into a *separate* activity is a **compression technique** — see [[the-method-compressed-solution]] — applied selectively (to components others build against, to break dependencies and parallelize), never universally in the base list.

> ID-prefix convention (load-bearing, not just recommended): `C-<abbrev>` the single per-component coding activity, `R-*` resource provisioning, `U-SPA*` SPA/webApp construction (the ONLY prefix classified as frontend downstream), `G-*` UI-design concepts, `I-*` integration, `N-*` noncoding (testing variants key on `N-STP`/`N-STH`/`N-PERF`/`N-IT`/`N-QA`). (`D###` design-first activities appear ONLY in the compressed solution, never the base.) In the typed model the activity `name` IS this short network id; the human-readable label goes in `title`. Downstream classifiers (DeriveType / DeriveVariant / ClassifyType) key on these prefixes — a prose name like `webapp-client-coding` classifies as a generic service.

Format each entry:

```markdown
| ID | Name | Type | Component | Role | Duration (days) | Depends on |
|---|---|---|---|---|---|---|
| C001 | OrderManager | coding | OrderManager | senior→junior (per-phase) | 20 | (its dependencies' contracts) |
```

The single duration covers the whole lifecycle (design + build + test-plan + integration phases); the profile weights (App A Table A-1) apportion it across phases. Dependencies are on other components' *activities* (their frozen contracts become available as their detailed-design phase completes).

**Sizing rules:**
- A component's single activity spans its full lifecycle; size it to the whole thing (design phase ≈ 20% + construction ≈ 40% + the rest per the App A weights).
- Construction durations vary by component size and layer. Typical:
  - Manager: 15–30 days
  - Engine: 10–20 days
  - ResourceAccess: 5–15 days
  - Resource (when we build it): 10–20 days
  - Client: 15–35 days (often the largest)
  - Utility: 5–15 days

### Step 2 — Integration is a phase, not an activity

**There is no separate `I-*` integration activity, per cluster or per use case.** Two independent reasons pin this down:

- **Table 11-1 has no integration activities at all.** The only activity Löwy's worked example gives a role in integration is activity 21, System Testing — and it depends on 5, 19, and 20 (construction activities), not on some intermediate integration node.
- **App A already makes Integration a *phase* of every activity's own lifecycle** (Requirements → Detailed Design → Test Plan → Construction → **Integration**). Each component's one coding activity already carries its own integration phase, dispatched when its dependencies' contracts are frozen. A separate `I-*` per relationship or per use case would charge the same work twice.

What App C's *"avoid integration at the end of the project"* buys you is already structural: because integration is a phase inside each activity, and each activity's dependencies are on other activities' frozen contracts (Step 1 above), integration happens incrementally as the network unfolds — never as a big-bang activity at the end.

The one activity that plays System Testing's Table 11-1 role is the terminal `N-IT` gate (Step 2b below): it depends on the top-of-stack construction activities (`U-SPA-*` for a product with a UI surface, or the top-layer `C-*`/`R-*` activities otherwise) exactly as activity 21 depends on 5/19/20 — never on a per-relationship or per-use-case `I-*`.

> **Suppressing a component's coding activity also suppresses every architecture edge through it — re-wire what replaced it.** This is the failure this rule can cause, and it is silent.
>
> Architecture relationships become network edges by way of the *activities* at each end. When a component's coding activity is suppressed (`constructionProfile: generated` or `provided`, Step 1), every relationship touching it has no activity to attach to and is dropped. For a client that is exactly the wrong outcome: the client→manager edges vanish, and the `U-SPA-<manager>` activities standing in for the client's real work inherit nothing.
>
> Observed live: the SPA activities and the terminal `N-IT` gate floated to the front of the schedule with 50 days of float, so the committed plan had **the whole system tested 50 days before the managers it tests existed**, and understated project duration by 43% (115 days against a true 165). Every gate was green — the network was internally consistent, just incoherent as a schedule.
>
> So whenever suppression removes a component that other work stands in for, wire the stand-in explicitly: emit `U-SPA-<m> → C-<m>` for every manager with a coding activity. Löwy has both edges — activity 19 (Client App1) depends on the managers, and activity 21 depends on the client activities.
>
> The general lesson is worth more than the specific edge: **a derived network can satisfy every consistency check and still describe an impossible schedule.** Solve it and assert an ordering invariant — the terminal gate must finish after everything it gates — because no amount of derive-and-compare will catch this.

### Step 2b — Standard UI-Design and Test-Plan activities

Two activities are **always emitted** (not left ad-hoc), because every plan needs them and their reviewers are fixed by role. Both are part of the derived baseline — `DerivePlan` emits them mechanically, the draft agent does not author them — but the *design intent* below is still the authoritative statement of what they are and why, since the derivation is only a mechanical enforcement of it.

**UI-Design activity (only for products with a UI surface — a Client + SPA/app container).** One UI-design activity derives, prefix `G-SPA`, role `ui-designer`, sequenced *before* the UI construction activities (the UI construction depends on it). The designer produces UI concepts; review is computed at construction time by `[[the-method-review-routing]]` (founder/architect-user + ux-reviewer + product-manager + architect) — do **not** stamp reviewers here.

| G001 | UI design concepts for the SPA | ui-design | reactSPA | ui-designer | 15 | (manager detailed-designs) |

**Testing activities (always).** Per Löwy's testing doctrine ([[the-method-testing]]) — unit testing alone is "borderline useless"; the load-bearing verification is full regression of the integrated system — emit, **not** BDD/Gherkin specs:

- a **System Test Plan** (`N-STP`, role `test-engineer`) — the ways to prove the integrated system fails, traced to the core use cases; early and high-float;
- a **System Test Harness** (`N-STH`, role `test-engineer`) — code that drives the system to break it (best-fit tech: Playwright for UI/SPA E2E, Go for API/integration; no Gherkin layer);
- a **Regression Test Harness** (`N-RTH`, role **`senior-developer`** — Löwy: regression harness is *developer-owned*, distinct from the test-engineer's system harness);
- **daily build + smoke** (`N-SMOKE`, role `senior-developer` — the roster has no devops class);
- a process **QA** activity (`N-QA`, role `qa-engineer`) — *"what will it take to assure quality?"*, distinct from test execution;
- a terminal **System Testing** gate (end-of-project, role **`software-tester`** — Löwy: testers run system testing; aim for a 1:1–2:1 tester:developer ratio).

Per-service test plans (STP) are written *before* each component's construction and live inside the construction activity — do not emit one activity per STP. Their review (`system-architect` + `product-manager` + `qa-engineer`) is computed at construction time by `[[the-method-review-routing]]` (`artifactKind: test-plan`).

| N-STP | System Test Plan (all core UCs) | noncoding | test-engineer | 15 | — |
| N-STH | System Test Harness (Playwright + Go) | noncoding | test-engineer | 20 | N-STP |
| N-RTH | Regression Test Harness | noncoding | senior-developer | 15 | N-STP |

Routing note: reviewer sets are **never** columns in this table — they are dynamic (see `[[the-method-review-routing]]`). This step only guarantees the *work* exists; who reviews it is computed when it is performed.

### Step 3 — Noncoding activities

Per ch. 13 (TradeMe second example), noncoding activities cluster at the beginning and end of the project. Walk through this checklist and add what applies.

**Beginning of project:**
- Requirements analysis (formal pass beyond `/system-design`)
- Architecture review with management
- Project planning (this very phase + downstream phases)
- System test plan + system test harness (test-engineer; early, high-float)
- Regression test harness (developer-owned)
- Quality-assurance process + gates (qa-engineer)
- Development environment setup
- Build / CI infrastructure + daily build & smoke
- Source control setup
- Database/schema design (the model, not RA code)
- Security review
- UX design (often a phase-long activity per ch. 11)

**Middle of project:**
- Code review activities (folded into construction in some teams; explicit otherwise)
- Documentation
- Architecture refinement / ADRs

**End of project:**
- System testing (terminal gate; run by software-tester)
- Performance testing
- Hardening / bug fix
- User acceptance testing
- Production deployment
- Training
- Documentation finalization
- Handover

Format:

```markdown
### Noncoding activities

| ID | Name | Type | Role | Duration (days) | Depends on |
|---|---|---|---|---|---|
| N001 | Requirements analysis | noncoding | product-manager | 10 | — |
| N002 | UX design | noncoding | ui-designer | 25 (spans entire UI phase) | N001 |
| N003 | Build/CI setup | noncoding | senior-developer | 10 | — |
| N004 | Production environment provisioning | noncoding | senior-developer | 15 | N003 |
| N005 | Integration testing | quality | test-engineer | 15 | (all construction done) |
| N006 | Hardening | quality | senior-developer + junior-developer | 10 | N005 |
| N007 | Deployment | noncoding | senior-developer | 5 | N006 |
| N008 | Training | noncoding | product-manager | 5 | N007 |
```

### Step 4 — Apply estimation rules (App C §4.4)

For each activity, verify:

| Rule | Check |
|---|---|
| Quantum of 5 days | duration is a multiple of 5 |
| No god activities | duration ≤ 35 |
| Resource assigned | role column not empty |
| Strive for accuracy, not precision | Don't estimate to 11.5 days; use 10 or 15 |
| Reduce estimation uncertainty | If you're guessing wildly, break the activity down |

If any duration > 35 days, split. Per ch. 12 §1: *"god activities" hide complexity and corrupt the network.*

### Step 5 — Overall project estimation cross-check

Per App C §4.4e: *"Estimate the project as a whole to validate or even initiate your project design."*

Use a broadband technique:
- Sum activity durations (total effort, person-days)
- Apply optimism reduction (typically multiply by 1.2–1.5 based on team's historical accuracy)
- Compare to your prior project estimation

If the sum is wildly different from a broadband estimate, something is off — either the activity list is missing things, or the estimates are biased.

Document the overall estimate in the `.activityList` model (rendered at the bottom of the activity list):

```markdown
## Overall project estimate (cross-check)

- Sum of activity durations: <N> person-days
- Broadband estimate (architect's gut): <N> person-days
- Reconciliation: <comment>
```

### Step 6 — Roles and phases table

Per ch. 11 Table 11-2 / ch. 13 Table 13-4, build the roles-and-phases mapping:

```markdown
## Roles and Phases

| Role | Phase 1 (design) | Phase 2 (build) | Phase 3 (integrate) | Phase 4 (harden) | Phase 5 (deploy) |
|---|---|---|---|---|---|
| Architect | X | X | X | X | X |
| Project Manager | X | X | X | X | X |
| Product Manager | X | X | X | X | X |
| Senior dev | X | X (incl. regression harness) | X | X | |
| Junior dev | | X (unit + STP tests) | X | X | |
| Test engineer | X (test plan + harness) | X (harness build) | X | X (perf) | X |
| Software tester | | | X (system test) | X (system testing) | |
| QA engineer | X (gates) | X (process audit) | X | X | X |
| UX designer | X | X | | | |
| DevOps | X | X | X | X | X |
```

Per Löwy ch. 9: the **test engineer** (builds harnesses, writes code to break the system), the **software tester** (runs system testing; 1:1–2:1 tester:developer ratio), and the **QA engineer** (senior, process — "what will it take to assure quality?") are three *distinct* roles. Do not collapse them.

The table keeps the book's row names; in the typed model the "UX designer" row is `ui-designer` and the "DevOps" row is `senior-developer` (the fixed roster has no devops class).

This is "a crude staffing distribution" (ch. 11) — it confirms which roles span the whole project and which are activity-specific.

## Draft-job doctrine (CI dispatch)

This is the normative task the CI draft job (and a local `/project-design` run) executes to produce the `.activityList` delta document. It is self-contained: everything a draft agent needs to author a sound set of deltas is stated here.

**The task is to author the deltas, not the list.** The baseline is derived, not drafted:

1. **Read the derived baseline.** It is a render-on-read of the committed System — `estimationEngine.DerivePlan` computes it deterministically; you do not construct it by hand and you do not need `putDraftModel` to produce it.
2. **For each derived activity whose band-midpoint default is wrong for this project, author an `ActivityOverride`** — `effortDays` and/or `riskBucket` only — with a written `justification` (see "Ground a justification in a property" below).
3. **Walk the ch. 13 noncoding checklist** (Procedure Step 3 above) and author an `AdditiveActivity` for each item that applies and isn't already in the always-emit inventory — environment setup, security review, documentation, training, deployment, and the like — each with its own incident edges (`dependsOn`) and a written `justification`.
4. **Do NOT author** `C-*`, `R-*`, `U-SPA-<manager>`, `G-SPA`, `I-*`, or the always-emit `N-*` inventory (`N-STP`/`N-STH`/`N-RTH`/`N-SMOKE`/`N-QA`/`N-PERF`/`N-IT`) — they derive. Typing one of these into the delta document is an anti-pattern (see "Anti-patterns to reject" below), not a completeness measure.

**The vocabulary is closed: no exclusions, no derived-edge overrides.** There is no way to say "this component needs no work" and no way to rewrite a derived dependency edge. If a component genuinely needs no work, it should not be a component — remove it from the System. If a derived edge is wrong, the *relationship* is wrong — amend the System's relationships, not the plan. Both routes go through system design, not through the activity-list delta; a silent exclusion or edge override is exactly how the zombies below survived undetected.

### What derives (the closed baseline)

Every activity in the baseline is named with its short network id under the fixed prefix conventions — downstream classifiers key on them — and DerivePlan computes it as a mechanical consequence of the committed System, with no drafting judgment involved:

- **`C-<id>`** — one per code-layer component whose `constructionProfile` is `"handwritten"` (the conservative default when unauthored — see "Components you do not build" below).
- **`R-<id>`** — one per Resource with `provisioning == "vendor"`.
- **`U-SPA-<manager>`** and **`U-SPA-S`** — one per Manager plus the SPA scaffold, when any component declares a UI surface.
- **`G-SPA`** — the UI-design concept, when any component declares a UI surface, sequenced before the `U-SPA-*` construction activities.
- **`N-*`** — the always-emit testing/QA inventory: `N-STP` system test plan, `N-STH` system test harness, `N-RTH` regression test harness, `N-SMOKE` daily build and smoke, `N-QA` process QA, `N-PERF` performance testing, and the terminal `N-IT` system-testing gate.
- **No `I-*`.** There is no integration activity, per relationship or per use case — see Procedure Step 2 above.

Generated client transport (an api/mcp/agent client whose substance is the platform-generated REST handlers, typed clients, MCP tool surfaces, and OpenAPI document) and platform/third-party-provided components both get no coding activity — see "Components you do not build" below.

### Doctrine corrections this project produced

These four rules were discovered during execution, each one after it prevented a real defect. State them explicitly because each is easy to "fix" back to the wrong shape if the reasoning behind it is lost.

**(a) Components you do not build get no coding activity — two kinds.** Generated client transport already got none (above). The second kind: a component backed by an **off-the-shelf platform or third-party service** is *configured*, not built, and also gets no coding activity. This project's four utilities are all of that kind — `security` → Keycloak, `diagnostics` → OTel, `logging` → a structured sink, `message-bus` → the Temporal substrate — and the derivation, before this correction, planned 4 coding activities for work nobody was doing.

State the distinction explicitly, because Löwy cuts the other way and a reader will notice: **Table 11-1 gives Logging and Security their own coding activities (6 and 7)** — because in that worked example they *are* being built. The rule turns on **who builds it**, never on which layer it sits in. It is expressed in the model as a component's `constructionProfile`: `"generated"` or `"provided"` suppress the coding activity, and it is unauthored components that must keep defaulting to `"handwritten"` — an unknown value conservatively emits work rather than silently deleting it.

**(b) No per-use-case integration activities.** See Procedure Step 2 above for the full reasoning (Table 11-1 has none; App A already folds integration into every activity's lifecycle). The consequence for this section: do not author "one `I-UC*` integration activity per core use case," and do not resolve the old contradiction between Step 2 (per component cluster) and this section (per core use case) by picking one — resolve it by deleting both, since neither matched the worked example and the correct answer is zero authored integration activities.

**(c) Ground a justification in a property, not a superlative.** Every override and additive activity carries a written `justification`, in this precedence: the committed title if it already explains the work; else a stated, checkable property of the system; else honest provenance saying the rationale was not recorded.

The failure mode worth naming: **every false justification this project produced was a superlative** — "tied for the most," "the smallest contract in the system," "the largest footprint of any engine." Each was falsified by a single counterexample somewhere in a 37-component system — rankings require checking every peer, and nobody did. The justifications that held stated a bare property and its consequence instead — "16 contract operations, past the App-C maximum of 12" — not a ranking. So: state the number and what follows from it; reach for a ranking only if you have actually checked every peer.

A false checkable claim is **worse** than honest provenance — it invites a reviewer to trust it and skip re-checking. "The rationale was not recorded; retained so the reviewed plan's numbers survive; flagged for re-estimation" is a *good* justification, not a failure to find one.

**(d) The drift gate replaces coverage checking.** The activity list is generated-and-committed, gated by `make derived-plan-check` — a target that re-derives the baseline from the committed System and fails the build on any difference, the same pattern as `contract.gen.go` and `make gen-models-check`. That is strictly stronger than the retired `ACT-COMPONENT-COVERAGE` rule, which could only ask whether every component appeared *somewhere* in the list; it could not (and did not) catch a component that had disappeared from the System while its activity zombied on in the plan.

### Worker classes are a fixed roster

WORKER CLASSES ARE A FIXED ROSTER, not open vocabulary: every worker class MUST be spelled exactly as one of system-architect, product-manager, project-manager, senior-developer, junior-developer, ui-designer, ux-reviewer, qa-engineer, test-engineer, software-tester — the Method team the platform actually dispatches. NEVER invent a domain-, component-, or platform-flavored class (no Capture-Engineer, no Platform-DevOps-Engineer): an unknown class silently rides default token rates in the cost engines and misclassifies in every downstream view. This applies to `AdditiveActivity.workerClass` exactly as it applied to the old fully-authored list. Typical assignment: junior-developer builds components and the SPA; senior-developer integrates and owns regression/CI/smoke/provisioning; system-architect owns schema and ADR work; ui-designer the UI-design concepts; test-engineer the system test plan, harness, and perf rig; qa-engineer the QA process; software-tester the terminal system-testing gate.

## Exit criteria (for router)

`.aiarch/state/project.json` → `.activityList` holds a committed typed `ActivityListDeltas` document with:
- `overrides`: zero or more `ActivityOverride`s, each naming an activity that actually exists in the derived baseline, each carrying a written `justification` (grounded per "Ground a justification in a property," not a superlative), each respecting the 5-day quantum / ≤35-day cap / Fibonacci risk bucket.
- `additive`: an `AdditiveActivity` for every ch. 13 noncoding checklist item that applies and is not already in the always-emit inventory, each genuinely componentless (no `componentId`), each with its own incident edges and a written `justification`, each `workerClass` from the fixed roster with a PlanningAssumptions `rateCard` entry.
- `additiveMilestones`, if any milestone depends entirely on additive work and therefore cannot derive.
- **No exclusions and no derived-edge overrides** — the vocabulary above is the whole document; there is no field for either.
- **No `C-*`, `R-*`, `U-SPA-*`, `G-SPA`, `I-*`, or `N-*` entries authored by hand** — `make derived-plan-check` re-derives the baseline from the committed System and fails the build on any difference; that drift gate, not this document, is what proves baseline correctness.

Move to `the-method-network-draft`.

## Anti-patterns to reject

- **Authoring a derived activity by hand.** A `C-*`, `R-*`, `U-SPA-*`, `G-SPA`, `I-*`, or `N-*` entry typed into the delta document is either a duplicate of the one `DerivePlan` already emits, or a **zombie** — an activity naming a component that no longer exists in the System. This is exactly how `C-HE`, `C-WIA`, and `R-WIT` survived in the committed plan, marked Done+Integrated, against components that had already been removed from the architecture. The drift gate (correction d, above) is what makes this impossible now; authoring one by hand routes straight around it.
- **An exclusion, or a derived-edge override, in any form.** The vocabulary is closed on purpose (see "no exclusions, no derived-edge overrides" above). Wanting either one is a signal to go fix the System, not the activity list.
- **Single "implement everything" activity** — god activity; split per component (enforced by derivation, not by hand).
- **A `D###` + `C###` pair per component in the base list** — this is the "clock." Detailed design is a *phase* of the one per-component activity, dispatched to the senior via the per-phase hand-off ([[the-method-handoff]]), not a separate activity. Separate design-first activities belong ONLY in the compressed solution ([[the-method-compressed-solution]]), applied selectively.
- **No additive activities for applicable checklist items** — projects don't ship without UX, infra, deployment, training. Walk the ch. 13 checklist; force the additive inventory for what the derivation cannot see.
- **A superlative justification** — "tied for the most," "the smallest X in the system." One counterexample falsifies it; ground the claim in a property instead (correction c, above).
- **Durations like 7, 11, 22 days** — break the quantum rule. Round to 5/10/15/20/25/30/35.
- **A single role for everything** — flatten the team's skill diversity; misses the senior-hand-off opportunity.
