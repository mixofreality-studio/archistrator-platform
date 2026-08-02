---
name: the-method-system-design-standard-check
description: System Design — Design Health, not a committed checklist. The ~40 mechanical Appendix-C rules run LIVE (methodcheck tier), render-on-read, never committed. This skill is the AGENT procedure for what is NOT machine-checkable: the waivers (conscious, justified exceptions recorded ON their host artifact) and the semantic attestations (Prime Directive, D1–D4, §3a–d) recorded on the System artifact. The Phase-1 seal is review-policy-conditional. Reads all Phase-1 slots from project.json; writes waivers/attestations onto .systemDesign and .volatilities (NOT a .standardCheck slot). Invoke as the last step of system design, before /project-design.
---

# Design Health (System Design gate)

**This is a teardown of the old committed "Standard Check" artifact.** There is no longer a `.standardCheck` slot to draft, no step-8 committed checklist, and no per-item PASS/WAIVED/FAIL table to author. The Appendix-C standard is still enforced — but it is split into three tiers, and only two of them involve an agent:

1. **Live tier (~40 rules, machine-only).** The mechanical Appendix-C rules — counts, cardinality, graph/layer rules, coverage joins, contract op-counts, consistency — run render-on-read as a pure function over the typed model, at `putDraftModel` (authoring gate), in `getDesignHealth` (the view), and in CI. They are **never committed**; they surface as passive health strips on the screen each rule concerns. An agent does **not** walk these — the machine does, continuously, so they cannot rot. See "The live tier" below for the inventory.
2. **Waivers (agent-authored, on the host artifact).** A conscious, justified exception to an in-scope rule is recorded as a `CheckItem` **on the artifact it qualifies**, not in a separate ledger — so the amendment→staleness loop re-opens the waiver together with its host. See "Waivers".
3. **Attestations (agent-authored, on the System artifact).** The genuinely semantic properties a machine cannot judge — the Prime Directive, Directives D1–D4, and §3a–d — are recorded as `CheckItem` attestations on `.systemDesign`, re-attested through the normal amendment → staleness → re-run loop. See "Attestations".

The customer-facing **Design Health view** (render-on-read, step 8 of the stepper) joins live findings + waivers + attestations in plain language. It is never committed.

## Canonical source

**Primary:** Löwy, Appendix C — Design Standard. Focus areas:

- §1 "The Prime Directive"
- §2 "Directives"
- §3 "System Design Guidelines"
- §6 "Service Contract Design Guidelines" (forward-look — full check during construction)

Cross-reference [[the-method-doctrine]] for the canonical Prime Directive + 9-directive wording, and [[the-method-layers]] for the layer/cardinality rules the live tier enforces.

## Input

State is git-as-DB: archistrator is a single Go-server repo whose canonical project state lives in `.aiarch/state/project.json` (a typed JSON aggregate). Markdown/DSL is a render-on-read of the typed state. The complete Phase-1 committed artifact set:

- `.mission`
- `.glossary`
- `.scrubbedRequirements` (Required Behaviors)
- `.volatilities`
- `.coreUseCases`
- `.systemDesign` (the typed `System`; read its rendered `architecture.dsl` where a check greps DSL)
- `.operationalConcepts`

## Output — NOT a slot

There is **no `.standardCheck` output**. This skill writes, when warranted:

- **Waivers** → nullable `waivers: CheckItem[]` on `$defs.System` (`.systemDesign`) and on `$defs.Volatilities` (`.volatilities`). A waiver is a conscious exception to an in-scope rule, recorded on the artifact whose content it qualifies.
- **Attestations** → nullable `attestations: CheckItem[]` on `$defs.System` (`.systemDesign`). An attestation is the architect's recorded judgement on a semantic property the machine cannot derive.

Both are the existing `CheckItem` shape (relocated, not new). They ride the host artifact's normal draft → review → commit path; there is no separate gate job.

## The live tier (machine-only — do NOT walk these by hand)

The ~40 mechanical rules below run as a pure function `(projectmodel slice + typed slots) → []Finding`, at authoring (`putDraftModel`), in the Design Health read-model, and in CI. They are informational strips on-screen, not agent tasks. Listed so you know what is ALREADY covered and must NOT be re-authored as a committed checklist:

- **Counts / cardinality (typed slots):** core-UC count 2–6; ≤5 Managers; subsystem bounds; RA ~10 bound; Resource ~10 bound; volatility count (informational — waiver-backed when exceeded).
- **Graph rules (System):** no up-calls; sideways only queued M→M / M→E; no layer skips (Client→Manager only); utilities reachable; Engines take no RA/IO edges; every Manager has ≥1 Engine; M→M queued only; Engines receive no queued; RA receives no queued; the pub/sub don'ts (bus-presence-conditional).
- **Call-chain correspondence (`CC-*` — the realization↔activity-diagram join; these run at ERROR and BLOCK System draft/commit):** every step keys a real node of the owning use case's diagram (`CC-VIEW-USECASE`, `CC-STEP-NODE`, `CC-STEP-UNIQUE`); every `action`/`timeEvent`/`acceptEvent` node carries a non-empty fragment and no ineligible kind carries one (`CC-COVERAGE`, `CC-STEP-NONEMPTY`); trigger↔entry alignment (`CC-TRIGGER-EVENT`) and `clientAction`-declares-an-actor (`CUC-ACTOR-REQUIRED`); endpoints resolve to a component or an owning-use-case actor (`CC-ENDPOINT-RESOLVES`); actor calls are actor↔Client sync only (`CC-ACTOR-EDGE`) and a laned node's fragment touches its actor (`CC-ACTOR-LANE`); every path roots legally and every later call continues from an already-reached endpoint (`CC-PATH-CONNECTED`); `decidedBy` is legally placed and resolvable (`CC-DECIDED-BY`). An unrealized use case cannot be committed. What these do NOT check is whether a fragment is TRUE — real calls, honest labels, meaningful order — that stays the architect's and the critique's judgment.
- **Static↔dynamic coverage (`DV-*`, computed over the union of fragments):** every component→component call matches a declared relationship on `(from, to, mode)` (`DV-EDGE-IN-MODEL`, `DV-EDGE-ENDS`, `DV-MODE`); one Manager entered from Clients per view (`DV-SINGLE-MGR`); unique view keys (`DV-KEY-UNIQUE`); every core component and every declared relationship exercised by some realization (`DV-STATIC-COVERAGE`, `DV-REL-COVERAGE`). Two deliberate exemptions are **emitted as Info lines rather than applied silently** — planned components (`DV-PLANNED-SKIPPED`) and utility-targeted relationships (`DV-REL-UTILITY-EXEMPT`, ambient dependencies no honest chain exercises). Read those lines: they are the proof an exemption was scoped rather than a rule quietly weakened.
- **Coverage / joins:** every core UC has a dynamic view (`ARCH-CHAINCOV`); `USECASE-DYNAMIC-MISSING`; `USECASE-ACTIVITY-MISSING`; **§mission-FIXED** — every volatility encapsulated by exactly one component or a ratified facet group (the fixed criterion — "exactly one owner or a ratified facet group", NOT the degraded "rationale names something"); every objective referenced by ≥1 behavior/volatility/decision; volatility `traces` resolve to Required-Behavior ids; `justifyingObjective` resolves to a live objective.
- **Contracts:** op-count sweet spot (≤12 hard cap); facet-contract component resolution; dead-op duplicate detection across facet groups (the D1–D4 mechanical class).
- **Consistency:** §dsl typed-model round-trip; §DEP-GRAPH-IDENTITY test-vs-prod component set; §temporal edge-label vocabulary (conditional on the durable-primitives decision); **slot-revision drift** — any finding computed against a basis older than the slot's current revision raises a drift warning (the rot-class killer).

**Promoted custom rules.** The three custom checks the old artifact carried that proved their worth — **§mission** (fixed criterion above), **§dsl** (round-trip), and **§DEP-GRAPH-IDENTITY** (test-vs-prod component identity) — are promoted INTO this live rule layer (they are no longer bespoke rows in a committed checklist). They run with the rest.

If a live rule is RED, the design is wrong: fix the offending typed artifact (`.systemDesign` / `.volatilities` / …) and re-stage it. You never "waive" a red mechanical rule by writing a passing row — either fix it, or (only for a genuine, justified structural exception like an intentionally-exceeded cardinality) record a **waiver** per below.

## Waivers (agent procedure)

A waiver is a **conscious, justified exception** to an in-scope rule — not a way to silence a real defect. Record it on the artifact it qualifies:

- **§2d** (golden Engines-to-Managers ratio, deliberately exceeded) and **§naming** (a deliberately-kept name that trips the naming convention) → `system.waivers` on `.systemDesign`.
- **§2h** (volatility count above the ~6–15 band, deliberately) → `volatilities.waivers` on `.volatilities`.

Each waiver `CheckItem` MUST carry:

- the rule id it waives (e.g. `§2d`);
- **justification** — the concrete, design-specific reason THIS system consciously accepts the exception (a waiver without a real justification is a FAIL, not a waiver);
- **re-eval trigger** — the condition under which the waiver must be reconsidered (e.g. "revisit if a 7th Engine is added" / "revisit if the volatility count crosses 20"). This is what lets the amendment→staleness loop re-open the waiver with its host.

Because the waiver lives ON its host artifact, amending that artifact marks the waiver stale and forces the architect to re-affirm or drop it — the waiver can never drift apart from the design it qualifies.

## Attestations (agent procedure)

Attestations are the semantic judgements a machine cannot make. Record them as `attestations: CheckItem[]` on `.systemDesign`, each with **real evidence** (an empty justification is the debt this teardown fixes — never attest without pointing at the evidence):

| Attestation | What you are affirming | Evidence to cite |
|---|---|---|
| **Prime Directive** | the architecture is volatility-based, not feature/domain — it will not have to change when requirements change | the volatility→component mapping; that no component is named after a use case or domain |
| **D1 — avoid functional decomposition** | no component named after a feature | the component roster |
| **D2 — decompose on volatility** | every component encapsulates exactly one volatility | `Encapsulates` values vs `.volatilities` (the live join backs this; the attestation is the architect's judgement that the mapping is meaningful, not just present) |
| **D3 — composable design** | non-core use cases compose from existing components without new ones | the non-core use cases' REALIZATIONS — each drawing cleanly, node by node, over the same roster the core use cases use. And the negative evidence, which is the load-bearing half: name any chain that resisted, and say how it was resolved WITHOUT adding a use-case-shaped component. "They all draw" is a weaker claim than "this one nearly didn't, and here is why the model still stands" |
| **D3 — smallest set (ch. 4 §2)** | the roster is the smallest set of components that composes to satisfy all core use cases; diminishing-returns reached — no smaller set survives the volatility coverage | the justification must engage honestly with the dozen-or-two order of magnitude: name what was consolidated (which candidates merged and why), which waivers bound the count, and what would shrink it further (and why that reduction was rejected — the volatility a smaller set would fail to encapsulate). A bare "it is small enough" is an empty attestation |
| **D4 — features as integration** | a new feature is a new orchestration, not a new component | the realizations showing features emerging from interaction — the same components, re-sequenced per use case. Cite two use cases whose fragments reuse the same calls in a different order |
| **§3a–§3d** | volatility decreases / reuse increases top-down; nature-of-business not encapsulated; Managers almost expendable | the layer ordering; the `.volatilities` "nature of business" filter results; the Manager-expendability walk |

D5's Phase-1 half ("design iteratively") is satisfied by the amendment loop itself — cite the loop as its evidence.

Re-attestation rides the normal loop: amending `.systemDesign` marks its attestations stale; the architect re-affirms each with current evidence before the artifact re-commits.

## The seal — review-policy-conditional

Phase 1 no longer seals on a committed checklist. The seal path consults the project's **review policy**:

- **vibes** — no ceremony. The phase seals automatically once the **live checks are green** and no unacknowledged waiver regressions exist. There is no human Design-Health ratification moment.
- **checkpoints / full** — an **explicit Design Health ratification** is required: a human reviews the Design Health view and ratifies. A **red live check** OR an **unacknowledged waiver** (or a stale attestation) **blocks the seal** until resolved.

The seal guard itself is server-side (not this skill's job to author) — but the agent's responsibility is to leave the design in a sealable state: green live checks, every waiver justified with a re-eval trigger, every attestation affirmed with evidence.

## Draft-job doctrine (CI dispatch)

There is **no standalone standard-check draft job that commits a slot** anymore. Where the old rail dispatched a `/standard-check-draft` job to author `.standardCheck`, the work is now:

- the **live tier** runs automatically (no agent) at `putDraftModel` and in CI;
- **waivers** and **attestations** are authored as part of drafting/amending their **host** artifact (`.systemDesign`, `.volatilities`) — i.e. the architect adds them in the architecture / volatilities draft jobs when a conscious exception or a semantic property needs recording, per this skill;
- the **Design Health view** is render-on-read (no draft).

So when you finish the architecture and volatilities artifacts, ensure any needed waiver sits on the right host with justification + re-eval trigger, and the semantic attestations sit on `.systemDesign` with evidence. Do not author a `.standardCheck` model — the slot is retired.

## Exit criteria (for router)

- Live checks are green (or every red is either fixed or covered by a justified structural **waiver** on its host artifact).
- Every waiver on `.systemDesign` / `.volatilities` carries a design-specific **justification** and a **re-eval trigger**.
- Every semantic **attestation** on `.systemDesign` (Prime, D1–D4, §3a–d, D5-Phase-1) is affirmed with **real evidence** — no empty justifications.
- Under **checkpoints/full**, the Design Health view has been ratified by a human; under **vibes**, the live checks are green and no waiver regression is unacknowledged.

System design is complete. Next: `/project-design <product>`.

## When to waive vs fix

**Waive when:**
- The deviation is intentional and traces to a business objective from the committed `.mission` artifact.
- The book itself acknowledges contexts where the rule may bend (e.g. an engines-ratio exceeded for a documented reason).
- The team has accepted the trade-off explicitly — and you can name the re-eval trigger.

**Fix when:**
- The violation reveals a bad decomposition.
- The violation breaks a Don't (Don'ts are rarely waivable — and most are red LIVE checks, not waiver candidates).
- The violation has no business objective backing it.

## Anti-patterns to reject

- **Re-authoring a committed checklist** — the `.standardCheck` slot is retired; do not emit PASS/WAIVED/FAIL rows.
- **Waiver without a re-eval trigger or justification** — that is a silent FAIL wearing a waiver's clothes.
- **Attestation with an empty justification** — the exact rot this teardown fixes; cite evidence or do not attest.
- **"Waiving" a red mechanical rule instead of fixing the model** — the live tier is not waivable by writing a passing row; fix the typed artifact.
- **Waivers/attestations parked away from their host** — they must live on `.systemDesign` / `.volatilities` so the amendment→staleness loop re-opens them with the design they qualify.
