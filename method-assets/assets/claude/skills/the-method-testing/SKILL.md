---
name: the-method-testing
description: Reference for Juval Löwy's testing & quality doctrine in The Method (Righting Software ch. 2/9/11/12/13/14, App A). The authoritative source for the testing strategy — test types, when tests are written, the three quality roles, and where testing sits in the activity graph. Use when another skill or activity needs the by-the-book testing classification. No BDD/Gherkin.
model: inherit
skills: the-method
---

# The Method — Testing & Quality Doctrine

The authoritative statement of how Löwy treats testing and quality assurance.
Other skills ([[the-method-activity-list]], [[the-method-review-routing]],
[[the-method-handoff]]) cite this rather than re-deriving it.

Canonical sources (Löwy, *Righting Software*): ch02
(unit vs regression), ch09 (roles, cost, daily build), ch11 (test activities in
the network), ch12 (quality multiplication), ch13 (TradeMe staffing), ch14
(the hand-off, smoke tests, QA), App A (per-service test plan).

## 1. Test types (and Löwy's stance)

- **Unit testing** — *necessary but "borderline useless" on its own* (ch02):
  defects live in the *interactions between units*, not the units. Never treat
  passing unit tests as system verification ("the streetlight effect").
- **Regression testing — the load-bearing verification** (ch02). *"The only
  way to verify change is full regression testing of the system, its
  subsystems, its components and interactions, and finally its units."*
  Volatility-based decomposition exists partly to make end-to-end +
  per-subsystem + per-component regression feasible.
- **Integration testing** — folded into regression, per use case.
- **System testing** — on this platform, not an activity: the union of passing
  per-component scenario runs (§ Deterministic component scenarios) is the
  proof that the system works.
- **Smoke tests** — daily clean build + power-up to "exercise the plumbing."
- **Black-box testing against the test plan** (per service, App A) — here the
  plan is the component's bound scenarios.

> **No BDD/Gherkin.** Löwy never mentions it. In aiarch we removed the BDD
> layer entirely. The load-bearing tests are **black-box integration tests**
> generated from each component's bound scenarios (see §7): **Go** scenario
> tests booting the real downstream stack through `scenariohost` (Temporal dev
> server, Postgres testcontainer, the in-process `FakeGitHub` + `LocalGitRepo`
> for GitHub, …); **Playwright** flows for
> any SPA/UI surface (browser-driven, hence inherently out-of-process even
> though TS). Hand-written and white-box tests in component packages are
> removed before merge and the `scenario-tests-only` gate rejects them (§7 R1).

## 2. When tests are written — test-PLAN-first, NOT TDD

Löwy is *test-plan-first*, not test-first. There is no red-green-refactor.

Per-service life cycle (App A, Table A-1):
`Requirements → Detailed Design → Test Plan → Construction → Integration`.

- The test-engineer binds the component's **derived scenarios** *before*
  coding — Löwy's *"list of all the ways the developer will later demonstrate
  the service does not work,"* here fixed by the use cases rather than invented.
  The test plan follows the design review (bindings need the frozen contract)
  and precedes construction.
- The generated scenario tests exist before construction; construction fills
  their hooks **in tandem with** the code.
- The venue runs the scenario tests against the real downstream stack on the
  activity's testing task; then regression is the union of every component's
  run.

The discipline: **plan the tests, then build code and tests together, then
integrate and regression-test.**

## 3. The three quality roles (do NOT collapse them)

Löwy (ch09) prescribes three *distinct* roles. QA ≠ testing.

| Role | What | Owns | Agent |
|---|---|---|---|
| **Test engineer** | A full engineer who writes code to *break* the system. Not a tester. *"Every software project should have a test engineer."* | Each component's scenario bindings on its test-plan task (early, high-float); a perf rig only when a justified additive calls for one | `test-engineer` |
| **QA engineer** | A *single senior* expert on the *process*: "what will it take to assure quality?" *Not* test execution. *"A sign of organizational maturity."* | Quality gates, process audit, defect taxonomy — a phase-spanning **role**, booked as indirect cost, not an activity | `qa-engineer` |

Löwy's third role, the **software tester** who *runs* system testing and
regression and files defects (ch09), has no agent here: the construction venue
runs the generated scenario tests and records the run on the activity's
testing task, so nobody is dispatched to run tests. Plus: **developers** fill
the hooks of the generated tests during construction. In Löwy's projects the
Regression Test Harness is developer-owned and the System Test Harness
test-engineer-owned (ch13); on this platform **the platform generates the
scenario tests from the bindings**, so neither harness is an activity.

## 4. Where testing sits in the activity graph

State is git-as-DB: these testing outputs are typed records in
`.aiarch/state/project.json`, NOT `designs/*.md` files. The per-component test
plan the test-engineer binds is `.phaseArtifacts.testPlan[<component>]`; the
venue's runs land in `.testingState.testRuns[]`; the perf rig, quality gates and
defects stay under `.testingState`. Per-activity status lives in
`.activityConstruction`.

Testing contributes **no activity of its own** to the network. Table 11-1's #4
(Test Plan) and #21 (System Testing) are carried by every coding activity's own
lifecycle (App A): the `test_plan` phase binds the component's scenarios after
the design review and before construction, and the `testing` review task inside
the `integration` phase gates on the venue's run of those scenarios at the
current revision. The network's sink is the project-end milestone, with every
terminal activity feeding it directly.

| Lifecycle task | Position | Owner | project.json target |
|---|---|---|---|
| `stp` — bind the component's derived scenarios | after `designReview`, before `construction` | test-engineer | `.phaseArtifacts.testPlan[<component>]` |
| `testing` — the venue's scenario run at the current revision | gate of the `integration` phase | the venue writes the run; reviewers read it | `.testingState.testRuns[]` |

**Not activities:**
- **Scenario tests** (Table 11-1 #5's harness) — generated by the platform from
  the bindings (`testgen`).
- **Daily build + smoke** — build automation is platform infrastructure (CI
  runs `GOWORK=off go build/test` from the module root).
- **QA process + gates** — the `qa-engineer` is a phase-spanning role, booked
  as indirect cost; its outputs still land in `.testingState.qualityGates` /
  `.testingState.qualityAuditReport`.
- **Performance testing** — not in Table 11-1's noncoding list; a project that
  genuinely needs it adds it as a justified additive activity
  (`.testingState.perfHarness`).

The Test Plan is an **early high-float enabler** — deferring it
"consumes 77% of [its] float… very risky" (ch11). The testing gate on each
activity's integration phase is where that value (~15% of project value, ch07)
is realized, and it largely resists compression.

### Deterministic component scenarios

Every test an archistrator-built app runs is **derived deterministically from
the use-case activity diagrams committed in `project.json`**, per Kim, Kang,
Baik and Ko's I/O-explicit activity-diagram technique (*Test Cases Generation
from UML Activity Diagrams*, SNPD 2007): each node is classified as input,
output, action or decision; every elementary path is enumerated, each loop
unrolled once, one stimulus per path (the single-stimulus principle). A
component's plan is every scenario in which some stimulus is an input to it.
The scenarios are derived on read and never stored — only the test-engineer's
**bindings** are committed, keyed by stable scenario id — and `testgen`
generates the black-box integration tests from them, leaving hooks for the
parts an agent must fill. The full design is
`docs/superpowers/specs/2026-10-02-deterministic-component-testing-design.md`
(archistrator repo); the mechanical rules are `framework-go/scenario`
(derivation), the TP-* methodcheck rules (binding completeness) and the
`scenario-tests-only` arch gate (no other test may exist in component code).

## 5. Layers and testing (structural, not type-per-layer)

Löwy does **not** map a test *type* to each layer. The mapping is structural:
- Each **service** (any layer) gets its bound scenarios + generated
  integration tests, run on its own testing task.
- The **system** gets no test activity of its own; the union of passing
  component runs is the proof, and the scenario tests are platform-generated.
- Features — and thus end-to-end testability — emerge only once the
  **Managers** integrate the lower layers (ch07), so a scenario whose upstream
  activity is not yet integrated is bound with a named `skip` until it is.
- aiarch specifics (governed by §7): invariants that *look* per-layer —
  **Engine** determinism, **ResourceAccess** idempotency (same-content →
  same-SHA) — are **promoted to an observable at the Client surface and asserted
  black-box**, never via a white-box test reaching into the package. Manager
  Temporal workflows, Client↔Manager wiring, and the SPA are all exercised
  through the **generated scenario tests against the real downstream stack**;
  the whole system through the union of those runs. Only a genuinely
  un-surfaceable invariant keeps one contract-level check, and it never gates
  alone.

## 6. Cost & economics

- Testing ≈ **15%** of project value (ch07), realized on each activity's
  testing gate. Within a service: Test Plan 10% + Integration 15% (App A).
- **Quality multiplies** (ch12): system quality = *product* of component
  qualities (10 components at 90% → 35% system quality). You cannot skimp.
- *"Quality is not free, but it does tend to pay for itself"* (ch14); quality →
  productivity → shorter schedule (ch09).
- The test plan and testing phases of each activity = **direct cost**. The QA
  engineer, like every phase-spanning role, = **indirect cost**; ongoing
  regression + daily build/smoke run on platform-generated tests and build
  automation, also **indirect cost**.

## 7. Test-authoring constitution (binding for archistrator-built systems)

Löwy's three roles are *distinct humans he trusts as professionals*. In aiarch
the "developer" and the "test engineer" are **AI agents**, and a single agent
writing both a component and its own test is a **cheating surface Löwy never
faced**: the test can be shaped to the implementation's shortcuts, or assert on
internals instead of contract behaviour. This constitution makes the role
separation and the black-box surface **structurally binding, not cultural**. It
applies to archistrator itself and to **every system archistrator builds**.

- **R1 — Every test is black-box against a published contract.** The only
  tests permitted in archistrator-built component code (managers, engines,
  resource access, clients, the UI app) are the generated scenario tests and
  their hooks file; any white-box or hand-written test made during construction
  is removed before merge, and the `scenario-tests-only` gate enforces it.
  White-box, in-package, internals-reaching tests are an anti-pattern *in
  Löwy's own terms* (App A's per-service testing is "black-box against the test
  plan"), not a tier we keep.
- **R2 — The load-bearing tier is the only routine tier.** The generated
  scenario tests drive each component's **contract** against the real
  downstream stack, organized **by core use case** (every scenario id is a path
  through one). Unit tests are "borderline useless" (ch02) → write none. A
  sub-surface invariant (Engine determinism, ResourceAccess idempotency) is
  verified by **promoting it to an observable** a probe can read and asserting
  it black-box — the promotion doubles as real observability. Only a genuinely
  un-surfaceable invariant gets one contract-level check, and **it never gates
  alone**.
- **R3 — The test host is a separate module and the tests are integration
  tests.** For Go systems the real downstream stack is booted by
  `framework-go-scenariohost` (Temporal dev server, Postgres testcontainer,
  the in-process `FakeGitHub` + `LocalGitRepo` for GitHub, …), a module outside
  the server package tree; the generated
  tests live beside the component as an external test package and talk to it
  only through its contract. UI surfaces are driven by Playwright against the
  integrated system. Placement is CI-asserted (R6).
- **R4 — Multi-surface systems get a cross-surface equivalence test.** When two
  Client surfaces expose the same operations over different transports (e.g. an
  HTTP API and an MCP server mirroring it method-for-method), a
  **transport-agnostic step layer** drives each core use case through *both* and
  asserts identical committed state. This is what keeps "mirrors method-for-
  method" honest as both surfaces evolve.
- **R5 — Author separation is binding.** The tests have no human (or agent)
  author: they are **generated** from the component's bound scenarios
  (`.phaseArtifacts.testPlan[<component>]`) by the platform's `testgen`. The
  scenarios themselves are not authored either — they derive from the committed
  use cases. What *is* authored is the **binding** — concrete values, expected
  results and probes — written by `test-engineer` on the component's test-plan
  task, and that authoring is **blind to the implementation**: it sees only the
  use cases + the component's contract, never the component source. The
  construction agent fills only the hooks the generator left; the venue runs the
  tests and records the run. **This separation IS the anti-cheat guarantee** —
  one agent must never author both a component and the plan that judges it;
  black-box surface alone is not enough without it.
- **R6 — Enforcement is mechanical.** The arch checker / CI asserts: (a) the
  `scenario-tests-only` rule — a component package holds no `_test.go` other
  than the generated scenario tests and their external-package hooks file;
  (b) `make gen-tests-check` — the generated tests match the committed bindings;
  (c) `…/internal/…` is never imported cross-tree (compiler-true, asserted for
  clarity). **Architecture-fitness tests (the layering arch checker) are not
  "tests" in this constitution's sense and stay** — they verify structure, not
  behaviour, and cannot be gamed by importing internals (that is the thing they
  forbid). The platform's own modules keep their tests; only archistrator-built
  component code is subject to this constitution.

## Anti-patterns

- **Unit tests as system verification** — they verify units, not interactions.
- **TDD / test-first ceremony** — Löwy is test-*plan*-first, not test-first.
- **BDD/Gherkin** — not in The Method; removed from aiarch.
- **Collapsing the three roles** — test-engineer ≠ tester ≠ QA.
- **Planning a test-harness, daily-build/smoke, or QA activity** — the
  harness and build automation are platform-generated; QA is a role, not an
  activity. Performance testing enters only as a justified additive.
- **Deferring the test plan** — it is an early high-float enabler.
- **Ignoring a red daily build + smoke** — a constant, defect-free codebase
  "accelerates the schedule like nothing else."
- **White-box / in-package internals-reaching tests** (§7 R1) — not Löwy (App A
  is black-box-against-the-test-plan), and the primary agent cheating surface.
- **One agent authoring a component *and* the bindings that judge it**
  (§7 R5) — the binding's author must be blind to the implementation; the tests
  themselves have no author at all, since they are generated from the bindings.
- **Inventing scenarios** — the set is derived from the use cases; a missing
  case is a missing use-case path, fixed in system design, not in the plan.
- **A hand-written `_test.go` beside the generated ones** (§7 R1) — the
  `scenario-tests-only` gate rejects it before merge.
