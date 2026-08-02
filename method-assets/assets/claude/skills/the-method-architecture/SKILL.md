---
name: the-method-architecture
description: Decompose the system into layered components and express the decomposition as a typed System model (components, relationships, a dynamic view for every use case — founder extension beyond Löwy's core-only). Call-chain validation iterates back into the decomposition — they are one activity, not two. Produces the typed System committed to project.json → .systemDesign (Structurizr DSL is a render-on-read). Use after core use cases, before operational concepts.
---

# The Method — Architecture

This skill produces the system's static architecture and validates it by tracing every core use case as a call chain. **Decomposition and the typed System model are a single loop**, not two phases: when a call chain fails to draw, the decomposition is wrong, and both the components and the model must be revised together. There is no intermediate component table — the typed `System` (`Components` + `Relationships` + `DynamicViews`) committed to `.aiarch/state/project.json` → `.systemDesign` is the only artifact. The Structurizr DSL is a render-on-read of that model, never the source of truth.

## Cross-cutting references

- [[the-method-layers]] — the canonical layer model, naming conventions, interaction rules, interaction don'ts, and cardinality limits. **This skill does not restate them.** When you need a rule, link there.
- [[the-method-doctrine]] — the Prime Directive and 9 directives. This skill operationalises Directives 1 (Avoid functional decomposition), 2 (Decompose based on volatility), 3 (Provide a composable design), and 4 (Features as integration, not implementation).
- `STRUCTURIZR-CONVENTIONS.md` (sibling file in this skill) — the Structurizr DSL conventions, tag conventions, edge-label conventions, and styling that govern the **render-on-read** of the typed `System` model. Read it before authoring the model. **Architecture diagrams are infrastructure- and platform-agnostic**: edge labels (the `Relationship.Label` field) must use the vocabulary of the destination layer (manager method, engine method signature, atomic business verb), never workflow-engine primitives (`Activity:`, `StartWorkflow(...)`, `SignalExternalWorkflow`) and never platform-specific commands (`git commit`, `ArgoCD reconcile`, `POST /charges`). Infrastructure and platform detail belong in the operational-concepts artifact (`.operationalConcepts`).

## Canonical source

**Primary:**
- Löwy, Chapter 3 "Structure" — layers, classification, layering rules, Design Don'ts.
- Ch. 4 §2 "Composable Design" — the smallest-set principle.
- Ch. 4 §2.2 "Architecture Validation" — call chains validate decompositions.
- Ch. 5 §4 "The Architecture" and §6 "Design Validation" — TradeMe worked example.

**Supporting:**
- Ch. 3 §4 "Classification Guidelines" — the Four Questions, naming.
- Ch. 3 §6 "Open and Closed Architectures" and §6.5 Design Don'ts.
- Appendix C §3 "System Design Guidelines" — items 2, 3, 4, 5, 6.

**Structurizr docs:** https://docs.structurizr.com/dsl.

## Input

State is git-as-DB: archistrator is a single Go-server repo whose canonical project state lives in `.aiarch/state/project.json` (a typed JSON aggregate). Markdown/DSL is a render-on-read of the typed state.

- The committed **volatilities** artifact → `.aiarch/state/project.json` → `.volatilities`
- The committed **coreUseCases** artifact → `.coreUseCases`
- The committed **glossary** artifact → `.glossary`
- The committed **mission** artifact → `.mission`

## Output

The typed **`System`** model (Go shape in `internal/resourceaccess/projectstate/system.go`: `Components []Component`, `Relationships []Relationship`, `DynamicViews []DynamicView`), committed to **`.aiarch/state/project.json` → `.systemDesign`**. It carries:

1. The static architecture (`Components` + `Relationships`).
2. One `DynamicView` — the use case's **realization** — per use case: **every** use case in the committed `.coreUseCases`, core AND every non-core variation (see the **Founder extension** callout below). Löwy validates only the core; the founder requires a call chain for every use case. A realization is **step-keyed**: one `CallStep` per realized activity node, each carrying an ordered fragment of calls (Step 9). It is not a flat edge list, and it carries no participant list — participants are derived from the call endpoints.

> **Founder extension (2026-07-05) — beyond Löwy.** Löwy's Method (Ch. 4–5) requires a call chain only for each *core* use case, and treats 2–3 non-core chains as an optional versatility demonstration. The archistrator founder extends this: **the committed architecture MUST carry a `DynamicView` for EVERY use case in the committed `.coreUseCases` set — core AND every non-core variation.** No use case may ship without a call chain. This is enforced as an ERROR at two points: methodcheck's `USECASE-DYNAMIC-MISSING` (the authoritative gate, run by `putDraftModel` while the agent authors and by the CI methodcheck gate) and the app-side read-back finding of the same id on the System review panel. It is the dynamic-view twin of `USECASE-ACTIVITY-MISSING` (every use case must also carry a non-empty activity diagram). The existing core-only rule `ARCH-CHAINCOV` stays as the Löwy-faithful core gate; `USECASE-DYNAMIC-MISSING` covers the whole set.

The Structurizr DSL (`architecture.dsl` / `workspace.dsl`) and any sequence-diagram markdown are **render-on-read** of this model, produced by the server's rendering access — never the source of truth, never files you hand-author. Per the two usage patterns (agentic/CI dispatch and local interactive), the agent emits the typed `System` JSON and commits it into `.systemDesign`; the server stages it (`StageArtifactForReview`) for the human review gate.

There is no intermediate "component table" and no hand-written `.dsl` file. The typed `Components` list *is* the table.

## The loop

This skill is one iterative loop:

```
classify volatilities → name components → build the System model → trace each core use case
                                ↑                                              │
                                └─────────── revise both ◄─────────────────────┘
                                       (if any trace fails)
```

A trace that cannot be drawn cleanly is a signal that the decomposition is wrong. Fix decomposition + model together. **Never** twist a use case to fit a bad decomposition.

## Procedure

### Step 1 — Classify volatilities into layer bins (the Four Questions)

Per Ch. 3 §4.2 "The Four Questions":

> *"Make a list of all the 'who' and put them in one bin as candidates for Clients. Make a list of all the 'what' and put them in another bin as candidates for Managers, and so on... The result will not be perfect... but it is a start."*

Walk every entry in the committed `.volatilities` artifact. Bin it (the book's ch. 3 §4.2 set is four questions — who / what / how / where; the table below is the platform's deliberate extension, splitting "how" into business activity vs resource access and adding a Utilities row):

| Question | Bin → Layer |
|---|---|
| Who interacts with the system? | Clients |
| What is required of the system? | Managers |
| How (business activity)? | Engines |
| How (resource access)? | ResourceAccess |
| Where (state)? | Resources |
| Cross-cutting concern with cappuccino-machine reusability? | Utilities |

If a volatility lands in two bins, it is either two volatilities or the volatility statement is ambiguous — refine the `.volatilities` artifact before proceeding. See [[the-method-layers]] for full layer identity rules.

**The mapping menu (ch. 2) — not every volatility mints a component.** Per Löwy, *"the transition from the list of volatile areas to components is hardly ever one to one."* Binning a volatility does not commit you to a component for it. Each committed volatility takes exactly one of three dispositions:

1. **Encapsulated by a component** — the normal case. A component encapsulates one or more volatilities (usually one; a component may encapsulate several closely related ones), and a volatility is normally owned by one component or a ratified facet group.
2. **Encapsulated by an operational concept** — some volatilities are contained by queuing, pub/sub, or another operational pattern rather than by a box in the static model. Record an explicit deferral disposition on the draft, resolved in the `.operationalConcepts` artifact. Silence is a defect, not a disposition.
3. **Encapsulated by a third-party service** — the volatility is bought, not built; the third party absorbs the change. Record the disposition.

A committed volatility with none of the three dispositions is a coverage gap (`DH-VOL-ENCAP-MISSING`).

### Step 2 — Name and classify each candidate component

For each bin entry, name the component using the conventions in [[the-method-layers]] (e.g., `<Noun>Manager`, `<Gerund>Engine`, `<Noun>Access`). Each becomes a typed `Component` (`Name`, `Kind`, `Encapsulates`, `AtomicBusinessVerbs`; `Layer` is derived server-side from `Kind`). Verify each component:

- Encapsulates **one or more** volatilities from `.volatilities` (usually one; a component may encapsulate several closely related ones). The human rationale goes in `Component.Encapsulates` (prose); the exact committed volatility name(s) that prose cites go in `Component.EncapsulatesVolatilities` (the typed machine join — see Step 6).
- Sits in **exactly one** layer.
- Passes the layer's identity test (e.g., Engines do no I/O; ResourceAccess exposes business verbs not CRUD; Utilities pass the cappuccino-machine test).

If a candidate fails its identity test, either re-classify it or split it. If you cannot decide, the volatility itself is probably wrong — return to `the-method-volatility-identification`.

### Step 3 — Check cardinality and smallest-set

Apply the cardinality limits from [[the-method-layers]] (≤5 Managers without subsystems, golden Engines-to-Managers ratio, ~10 components order-of-magnitude, ≥8 Managers is a hard fail).

Then apply the smallest-set test from Ch. 4 §2:

> *"Once you cannot think of a smaller set of building blocks, you have found your best design."*

If you can reduce further without losing a volatility, reduce.

### Step 4 — Reject anti-patterns

Before building the model, scan the candidate set for these smells. Any hit means restart from Step 1 with the smell as a guard:

| Anti-pattern | Indicator |
|---|---|
| Functional decomposition | Components named after features (`OrderProcessing`, `Reporting`, `Notifications`) |
| Domain decomposition | Components named after domains/entities (`UserService`, `ProductService`) |
| God service | One Manager doing everything |
| Services explosion | One component per use case |
| Chained services | A→B→C where B presumes A or C |
| Speculative encapsulation | Component for a future need with no current volatility |
| Reflex components | `Logging`/`Reporting` declared by reflex with no specific volatility behind it |

### Step 5 — Read the render conventions

Open `STRUCTURIZR-CONVENTIONS.md` (sibling to this SKILL.md). It defines how the server renders the typed `System` to Structurizr DSL (the `workspace`/`model`/`views`/`styles` blocks, per-layer tags `client`/`manager`/`engine`/`resource-access`/`resource`/`utility`, description style, edge-label conventions). You do not author the DSL — you author the typed `System` model so that the render conventions are satisfiable. Read them before building the `Components` and `Relationships` so your component names, descriptions, and edge labels conform.

### Step 6 — Author a `Component` entry for every component

For each component identified in Step 2, add a typed `Component` to `System.Components`:

- `Name` — the component name (e.g., `OrderManager`); the server assigns `ID = Slug(Name)`.
- `Kind` — one of the closed taxonomy (`Client`/`Manager`/`Engine`/`ResourceAccess`/`Resource`/`Utility`); the server derives `Layer` from `Kind` — you never emit a layer.
- `Encapsulates` — the volatility (usually one; occasionally several closely related ones) this component owns (Manager/Engine/RA); `""` for Resource/Utility. **≤ 150 characters** when it renders as the on-element description: name the volatility plus a brief verb-phrase. Implementation detail (retention, idempotency mechanics, schema, rationale) belongs in the `.operationalConcepts` artifact or the `.volatilities` artifact — NOT here. See "Description style" in `STRUCTURIZR-CONVENTIONS.md`.
- `EncapsulatesVolatilities` — the typed machine join: the **exact committed volatility name(s)** from `.volatilities` that the `Encapsulates` prose cites, verbatim. The prose stays (human rationale); this list is what the server joins on. Every Manager/Engine/ResourceAccess records at least one; Resource/Utility leave it empty. A name that matches no committed volatility is a dangling reference (`DH-COMP-VOL-DANGLING`); a Manager/Engine/ResourceAccess with an empty list is `DH-COMP-NO-VOLATILITY`.
- `AtomicBusinessVerbs` — for ResourceAccess, the atomic business verbs it exposes.

Also capture every actor named in core use cases (rendered as `person` declarations).

### Step 7 — Add only the relationships needed for core use cases

Do NOT exhaustively wire every plausible edge. Add only the `Relationship` entries (`From`, `To`, `Mode`, `Label`) to `System.Relationships` actually exercised by core use cases (Step 8 will exercise them).

Every relationship must comply with the closed-architecture rules in [[the-method-layers]]: no calling up, no sideways within a layer except queued Manager→Manager, no skipping layers, plus the don'ts (Engines don't receive queued calls, Engines/ResourceAccess/Resources don't publish events, etc.).

Mark sync vs queued in `Relationship.Mode` (`CallSync` / `CallQueued`) — the renderer styles each distinctly.

**Edge-label vocabulary.** `Relationship.Label` uses the vocabulary of the **destination layer's responsibility**. The architecture stays infrastructure- and platform-agnostic; the infrastructure-specific primitives go in the `.operationalConcepts` artifact. See `STRUCTURIZR-CONVENTIONS.md` "Edge-label conventions" for the full table and the rule-of-thumb test. Quick summary:

| Edge | Label shape |
|---|---|
| Client → Manager | `<managerMethodName>(<args>) → <result>` |
| Manager → Engine | `<EngineMethodName>(<args>) → <output>` |
| Manager → ResourceAccess | `<atomicBusinessVerb>(<noun>)` (e.g., `appendEvent(OrderSubmitted)`) |
| Manager → infrastructure-access ResourceAccess | atomic verbs in the infrastructure's *generic* domain (e.g., `awaitSignal(reviewDecision)`, `scheduleNextActivity`) — never workflow-engine product primitives |
| Manager → Manager (queued) | `delivers <SignalName> (queued)` |
| ResourceAccess → Resource | resource-domain verb + idempotency note — no platform-specific commands (no `git commit`, no `INSERT … ON CONFLICT`, no `POST /charges`) |

### Step 8 — The static-architecture view (render-on-read)

The static-architecture view (Clients at top, Resources at bottom — the layered pyramid) is derived automatically from `System.Components` + `System.Relationships` by the renderer; you do not author a separate view declaration. Just ensure the components and relationships are complete and well-tagged so the rendered top-to-bottom layout is correct. See `STRUCTURIZR-CONVENTIONS.md` for the rendered shape.

### Step 9 — Realize every use case: author the step-keyed call chain (the validation)

> **Composable-design caution (ch. 4 §2).** Löwy's rule: *"a composable design does not aim to satisfy any use case in particular."* The decomposition targets VOLATILITIES; the realizations VALIDATE that the components compose to satisfy each use case — they must never DRIVE the decomposition. Never add, split, or reshape a component so that a particular chain draws; that is functional decomposition re-entering through the validation door. The platform's every-use-case chain mandate (founder extension, below) raises the validation bar — every use case, core and non-core, must compose — but it does not change this rule: if a chain won't draw cleanly, fix the decomposition against the volatilities, don't add a use-case-shaped component. **A chain that will not draw is a STOP, not a licence to edit the static model:** state the obstruction, and let the missing edge or component justify itself independently — as volatility or business semantics, ratified on its own merits — or not at all.

For **every** use case in the committed `.coreUseCases` — core AND every non-core variation (founder extension; see the callout under **Output**) — add a typed `DynamicView` to `System.DynamicViews`. The view is the use case's **realization**: it says, per activity node, which calls that node makes.

```go
type DynamicView struct {
    UseCaseID string     // the UseCase this realizes
    Key       string     // stable view key, e.g. uc1-drive-system-design (a deep-link anchor — never renamed)
    Title     string     // names the use case
    Steps     []CallStep // one per REALIZED activity node
}

type CallStep struct {
    ActivityNodeID string      // a node of THIS use case's activity diagram
    Calls          []TraceCall // ≥1, ORDERED — the fragment that realizes the node
}

type TraceCall struct {
    From  string   // Component.ID, or an Actor.ID of the owning use case
    To    string   // same
    Mode  CallMode // sync | queued
    Label string   // destination-layer vocabulary (Step 7's table)
    Alt   *string  // optional alt-group tag — see BOTH-SURFACE RULE
}
```

This is the **call chain** of Chapters 4 and 5, keyed to the activity diagram that states the required behavior. Tracing it cleanly proves the decomposition supports the use case.

**There is no participant list and no flat edge list.** Participants are DERIVED from the union of call endpoints; the sequence is DERIVED by walking the activity graph. Paths are never stored — you author fragments, the walker composes them. Do not try to author a linearization.

**Step eligibility by node kind (`CC-COVERAGE`):**

| Node kind | Step |
|---|---|
| `action`, `timeEvent`, `acceptEvent` | **MUST** have one — these are the nodes that DO something |
| `decision`, `switch` | **MAY** have one — author it only when evaluating the guard itself requires a call (asking an Engine for the verdict you branch on). Decision steps are coverage-neutral |
| everything else (`start`/`end`/`merge`/`fork`/`join`/`swimlane`/`note`/`loop`/`goto`/`interruptEdge`) | **MUST NOT** — attaching a step to one is an error, not a nicety |

At most one step per node (`CC-STEP-UNIQUE`); every step has ≥1 call (`CC-STEP-NONEMPTY`); every `ActivityNodeID` resolves to a node of the owning use case's diagram (`CC-STEP-NODE`).

**Actors are participants.** Every endpoint resolves to exactly one of {a `Component.ID`, an `Actor.ID` of the owning use case} (`CC-ENDPOINT-RESOLVES` — no `buildStatus` exemption: a planned component resolves by roster id like any other). Persons appear in the chain, C4-style, above the Client tier. A call touching an actor **must** have a Client-layer component on the other end and mode `sync`; actor↔actor is illegal (`CC-ACTOR-EDGE`). The rule permits either direction, but **doctrine allows only `actor → Client`**: never draw a `Client → actor` "render" call, because **call chains draw calls, never returns** (the book's chains carry none, and neither does this corpus). A node whose entire content is the rendered result of an already-drawn call is a response-render note, not an action.

**The entry-call convention (there is no "user initiates" node).** Diagrams do not carry a node for "the user opens the app", so the entry calls ride the FIRST action or event step of the path:

| Path entry | The step's `calls[0]` must be |
|---|---|
| `start` (a `clientAction` use case) | `actor → Client` (sync) — the ONLY legal root for a start path. The Client→Manager entry call follows in the same step |
| `timeEvent` (a `timer` use case) | the scheduling `Client → Manager` call (e.g. `scheduler-client → billing-manager`) |
| `acceptEvent` (a `busMessage` use case) | the **queued** call into the receiving Manager — the queued Manager→Manager edge itself, as `calls[0]` |

Mid-chain `actor → Client` re-entry (a human gate, an operator escalation) is always legal and starts a fresh fragment. Every other call's `From` must already be reached by an earlier call on that path (`CC-PATH-CONNECTED`; the walker enumerates decision branches, takes loops once, and walks fork branches in declared order).

**A node carrying `linkedActorId` must have that actor as an endpoint in its step** (`CC-ACTOR-LANE`). Only Client-touching human actors carry `linkedActorId`; external systems are lanes by `roleName` alone.

**BOTH-SURFACE RULE (binding).** A step realizing a human entry — the `actor→Client` and/or `Client→Manager` calls — draws **both** surfaces whenever the Manager operation rides both static Client→Manager edges. Tag them with `Alt` so they render as lettered siblings of one ordinal (`1a`/`1b`) rather than a sequence: by convention `"s1"` is the actor→Client pair and `"s2"` is the Client→Manager pair. A single-surface realization of an operation that both surfaces carry is a **defect unless a product justification is recorded** — the static model asserting equivalence and the realization denying it is drift. Constraints: every call in one alt group must target the same Manager (alternatives read as concurrent under the cardinality don'ts), and all Client entries in the view must still enter one and the same Manager (`DV-SINGLE-MGR`).

**Alt groups are step-local.** The `Alt` value groups calls **within one step** and nothing else; no cross-step surface pairing may be inferred, and none should be authored. Cross-step correlation is semantically empty here — the requirement the surfaces satisfy asserts they are EQUIVALENT, so an entry begun on one surface may legally continue through the other. When a diagram splits the human gesture (one node) from the system entry (the next node), each step legitimately carries its own group — an s1-only step followed by an s2-only step is correct, not a half-authored pair.

**Within-step call order is load-bearing.** The root call is `calls[0]`; the rest read in execution order (open the rail before dispatching the job; write before the substrate hop). The steps-array order is load-bearing too — downstream joins (test-plan entry-operation coverage, deep links) derive from it. Reordering a fragment changes what the model claims.

**`decidedBy` — who makes the call at a branch.** `decision`/`switch` nodes may carry `decidedBy` (authored on the `.coreUseCases` activity node, resolved here against the roster). It names the decider — a `Component.ID` or an `Actor.ID` of the owning use case — and drives the trace UI's "Decided by <name>" highlight. Precedence when the UI resolves a decider: **explicit `decidedBy` → the node's actor lane → the entry Manager (inference).** Author it explicitly whenever the honest decider is NOT the entry Manager:

- the verdict IS an Engine's output (`intervention-engine`'s Retry|Escalate|Takeover, an autoscaler's proposal) → name the Engine;
- the branch turns on a human's act (approve/reject, revoke, take over) → name the actor;
- the branch turns on an external provider's verdict → name the Resource component that renders it;
- the guard is the Manager's own state read → leave it ABSENT; the entry-Manager inference is the honest attribution, and a wrong explicit value is worse than none.

**Placement rule:** the decider must be a plausible endpoint of the realization — for a component, the chain can reach it over a declared static edge; for an actor, the diagram has an actor→Client touchpoint. Naming an unreachable component is over-attribution (`CC-DECIDED-BY` catches illegal placement and unresolvable values, not dishonest ones — that is your judgment).

**Verb calls draw; deliveries don't.** A call to a Utility IS drawable — and draws a real edge in the trace — exactly when the utility verb IS that activity node's business work (registering a per-customer schedule on an onboarding chain). But a signal **delivery** whose business meaning is already a declared queued Manager→Manager relationship is realized as **that queued call and never additionally as a bus call**: the bus is the medium of the queued edge, not a party to it. Drawing both double-counts one act and re-materializes the substrate the layering deliberately hides. Startup/boot-time wiring that no use-case flow performs is drawn nowhere.

**Note-honesty.** A node with no honest realization becomes a **note** — never a fabricated call. Two legitimate shapes: an **in-flow note** (edges intact) for an out-of-band or response-render step, and a **free-floating, edge-less note** for ambient context or an outside-the-system-boundary effect. Notes carry no steps and no coverage obligation, which is exactly why the conversion is dangerous: `note` is the cheap way past a hard step. **The activity diagram is a requirements artifact, and the gate must never become the reason it changes.** Convert a node to a note only when the system genuinely performs no work there — a precondition, an external convergence, an owner-side act outside every Client, the render of an already-drawn call's result — and say so in the note's label, naming where the real work IS realized when it is realized elsewhere. A conversion made in response to a finding needs the same architect judgment as a conversion made on the merits; "it wouldn't draw" is not a justification.

**Downstream proportionality.** The gate validates the decomposition; it must never become the reason the model changes. That cuts both ways, and both failure modes are the futility-of-requirements trap wearing the gate as a mask:

- never add, split, or reshape a **component** so a chain draws (the caution above);
- never mass-produce shallow use cases to feed the gate, and never skip capturing a real use case to dodge the realization cost. The Method wants 2–6 core use cases plus the variations that genuinely matter — the gate covers **what is authored**, and authoring more or less than the design honestly needs corrupts both artifacts.

**Renames are cross-slot events.** Because realizations and `decidedBy` reference component ids, a component rename touches **every slot that names it** — the `.coreUseCases` `decidedBy` values, the realization calls, the static relationships, the activity-list titles — and they must all move in ONE amendment. A rename landed in the static model alone leaves dangling ids that fail `CC-DECIDED-BY` / `CC-ENDPOINT-RESOLVES` / `DV-EDGE-IN-MODEL` and, worse, can pass a slot-scoped check while the whole model is inconsistent. Plan the rename as a checklist of every referencing site before you make the first edit.

**Suspend/resume use cases.** If the use case suspends waiting for an external event and resumes, do not draw the execution substrate's own primitives — a Manager's timers, awaited signals, and child executions are how it runs, not calls between components. The suspension is implied by fragment order: the Manager's last pre-suspend verb is followed by the resuming `actor → Client → Manager` fragment on the node that receives the human's action.

**Per-view validation rules.** These run in the live design-health tier AND block System draft/commit at **Error** severity — the realization is not documentation you can defer.

| Rule | If it fires, the failure means |
|---|---|
| `CC-VIEW-USECASE` | The view names a use case that is not in the committed set — a stale key or a deleted use case |
| `CC-STEP-NODE` | A step keys a node that is not in this use case's diagram — the two artifacts have drifted |
| `CC-STEP-UNIQUE` | Two steps key one node — split fragments; merge them, order matters |
| `CC-COVERAGE` | An `action`/`timeEvent`/`acceptEvent` node has no step (the decomposition cannot perform a required step, or the node is a note in denial), or a step hangs off an ineligible kind |
| `CC-TRIGGER-EVENT` | Trigger↔diagram misalignment: a `timer` use case with no `timeEvent` entry, a `busMessage` with no `acceptEvent` entry, or a `clientAction` carrying event entries. Either the taxonomy or the diagram is lying |
| `CC-STEP-NONEMPTY` | An empty fragment — there is no "no-call" escape hatch; author the calls or make the node a note |
| `CC-ENDPOINT-RESOLVES` | A dangling or ambiguous endpoint id — usually a rename that did not land cross-slot |
| `CC-ACTOR-EDGE` | An actor call that is not actor↔Client sync — a layer violation reaching past the Client tier |
| `CC-PATH-CONNECTED` | A fragment that neither roots its entry legally nor continues from a call already made on that path — the chain is broken, or the root shape contradicts the trigger |
| `CC-ACTOR-LANE` | A node laned to an actor whose step never touches that actor — the swim lane claims a participation the calls deny |
| `CC-DECIDED-BY` | `decidedBy` on a non-`decision`/`switch` node, or a value resolving to neither a component nor an owning-use-case actor |
| `CUC-ACTOR-REQUIRED` (coreUseCases-scoped) | A `clientAction` use case with zero declared actors — nothing can root its chain |
| `DV-EDGE-ENDS` / `DV-EDGE-IN-MODEL` / `DV-MODE` | A component→component call with no matching declared relationship on `(from, to, mode)`. Actor calls are exempt by design; labels are free per call (several steps may reuse one static relationship with different labels) |
| `DV-SINGLE-MGR` / `APPC-INT-CLIENT-MULTI-MGR` / `APPC-INT-MGR-MULTI-QUEUE` | Don't 6a — Clients entering more than one Manager in a use case, or a Manager queueing to more than one Manager in the same use case; decomposition wrong |
| `DV-STATIC-COVERAGE` / `DV-REL-COVERAGE` | A core component or a declared relationship that no realization ever exercises — dead architecture, or a missing realization. Computed over the union of all fragments. **Utility-targeted relationships are exempt** (ambient dependency, never exercised by design) and the exemption is emitted as an `DV-REL-UTILITY-EXEMPT` Info line rather than applied silently — read that line, it is the proof the exemption was scoped and not a suppression |
| `DV-KEY-UNIQUE` / `DV-PLANNED-SKIPPED` | Duplicate view keys; planned components skipped by coverage (surfaced, not silent) |

Judgment rules the machine does NOT check — yours to enforce: every fragment's labels use the destination layer's vocabulary (no `Activity:` / `StartWorkflow(` / `git commit` / `POST /endpoint`); every step has a meaningful action label, not a generic verb; and the last fragment produces the outcome the use case names.

**If any rule fails, the decomposition is wrong — not the use case.** Return to Step 1, revise the component set and relationships, and re-realize. Iterate until every use case realizes cleanly.

**Cross-Manager symmetry (do this once, after all views realize).** Ch. 3: *"Symmetry is so fundamental for good design that you should generally see the same call patterns across Managers"*; ch. 5 notes *"the high degree of self-similarity or symmetry in the call chains."* The rules above check each view's INTERNAL structure — nothing compares views to each other. So compare them yourself: per Manager, line up entry shape, Engine-consultation pattern, ResourceAccess fan-out, Resource-hop idiom, queued edges, and the granularity at which comparable steps are drawn. Then ask the book's question of every difference — *"Why is the fourth case different? What are you missing or overdoing?"* Each asymmetry either earns a justification naming the volatility behind it, or it is a defect: an architectural one (fix the model) or a drawing-level one (two realizations describing identical code at different granularities — align them).

### Step 10 — Cover every non-core use-case variation (founder extension)

Per Ch. 5 §6, Löwy uses 2–3 non-core call chains to *demonstrate* that the architecture also handles non-core use cases without modification. **The founder extension makes this mandatory and total (2026-07-05):** every non-core use-case variation in the committed `.coreUseCases` must carry its own `DynamicView` in the same `System` model — not a representative sample. A System draft that leaves any use case (core or non-core variation) without a call chain is rejected by `USECASE-DYNAMIC-MISSING` (methodcheck ERROR at `putDraftModel` + CI, and the app-side read-back finding on the review panel).

If a non-core use case cannot be drawn, the decomposition is missing a volatility — return to `the-method-volatility-identification` before continuing.

### Step 11 — Where order, duration, and multiplicity live

Ch. 4 reaches for a supplementary sequence diagram when the order of calls between components is non-obvious, when duration or SLA per call matters, or when one component participates several times (TradeMe used one for Terminate Tradesman, Fig 5-28). **The step-keyed realization already carries the first and the third**: fragments are keyed to the activity nodes, the walker derives the order from the diagram's own branches and loops, and a component participating repeatedly is simply named in several fragments. Author the order honestly in the fragments and you have the sequence diagram's content, joined to the requirements instead of floating beside them.

The typed model carries **no sequence-diagram source field** — do not attempt to attach one to a `DynamicView`. What genuinely does not fit the realization is **duration/SLA and replay semantics**: those are operational properties, and they belong in the committed `.operationalConcepts` artifact, not in the architecture. Use the same restraint the book asks for — over-spec is its own anti-pattern.

### Step 12 — Apply the Structurizr Conventions validation checklist

Walk the rules table from `STRUCTURIZR-CONVENTIONS.md` against your typed `System` model. Every row must pass. Any failure either fixes here or sends you back to Step 1.

### Step 13 — Commit the typed `System` to `.systemDesign`

There is no `workspace.dsl` to copy and no `.dsl` file to author. Commit the typed `System` model into `.aiarch/state/project.json` → `.systemDesign`. The server's rendering access derives `architecture.dsl` / `workspace.dsl` (and any sequence diagrams) render-on-read from this slot; the Structurizr DSL is never a hand-maintained file.

### Step 14 — Validate the model (render + parser gate, server-side)

This is a hard gate. When the typed `System` is staged/committed, the server renders it to Structurizr DSL and runs the strict parser as part of artifact validation (the parser has traps — `styles` block syntax, dynamic-view edges not declared in the model — that the typed model is shaped to avoid). A render or parse failure means the model is malformed — fix the `Components` / `Relationships` / `DynamicViews` and re-stage. Do NOT advance to operational concepts with a model that does not render+parse cleanly.

If a build is involved when running the server locally, the Go build is `GOWORK=off go build ./...` / `go vet ./...` / `go test ./...` from the module root.

## Draft-job doctrine (CI dispatch)

This is the normative task the CI draft job (and a local `/system-design` run) executes to produce the `System`. It is self-contained: everything a draft agent needs to draft a sound architecture is stated here.

Decompose the system by VOLATILITY into layered components, then validate by drawing the call chains. Bin each volatility with the Four Questions: Who -> Client, What -> Manager, How(activity) -> Engine, How(resource) -> ResourceAccess, Where(state) -> Resource, cross-cutting reuse -> Utility. Each Manager/Engine/ResourceAccess component encapsulates ONE OR MORE volatilities (usually one; several only when closely related) and sits in EXACTLY ONE layer; Component.Layer MUST equal Component.Kind. The volatility-to-component transition is hardly ever one to one (ch. 2): a volatility is normally owned by one component or a ratified facet group, but it may instead be encapsulated by an OPERATIONAL CONCEPT (queuing, pub/sub — record an explicit deferral disposition on the draft, resolved in the operationalConcepts artifact) or by a THIRD-PARTY service; not every volatility mints a component, but every committed volatility must take one of those three dispositions (a gap is DH-VOL-ENCAP-MISSING). On every Manager/Engine/ResourceAccess set `encapsulatesVolatilities` to the EXACT committed volatility name(s) its Encapsulates prose cites — the prose is the human rationale, the typed list is the machine join (a name matching no committed volatility is DH-COMP-VOL-DANGLING; an empty list on a Manager/Engine/ResourceAccess is DH-COMP-NO-VOLATILITY). Obey closed layering: calls go downward only, never upward, never sideways except queued Manager->Manager. REJECT functional decomposition (components named after features) and domain decomposition (components named after entities) — name components after the volatility they hide. Keep it small: order-of-magnitude ~10 components, Managers <=5, fewer Engines than Managers. Emit one dynamicView per use case — CORE and SUPPORTING (nonCore) variations ALIKE — REALIZING its call chain (exactly one Manager entered from the Client; every call labelled in the destination layer's vocabulary, not infrastructure terms). FOUNDER EXTENSION (beyond Löwy, who validates only the core): EVERY use case in the committed CoreUseCases set MUST carry its own dynamic view — you may NOT ship the architecture with any use case (core or a nonCore variation) left without a call chain. If a use case cannot be drawn cleanly, the DECOMPOSITION is wrong — fix the components, not the use case; and if the honest fix is a new component or edge, STOP and escalate rather than reshaping the model to make a chain draw.

REALIZATION SHAPE (step-keyed — Step 9 is the full doctrine, this is the emit contract): a dynamicView carries `steps`, NOT a participants list and NOT a flat edge list. Each step is `{activityNode, calls[]}` where `activityNode` names a node of THAT use case's activity diagram and `calls` is an ORDERED, NON-EMPTY fragment of `{from, to, mode, label, alt?}`. `from`/`to` are component NAMES or an ACTOR ROLE of the owning use case. Every `action`, `timeEvent`, and `acceptEvent` node MUST carry a step; `decision`/`switch` MAY (only when evaluating the guard itself requires a call); every other kind MUST NOT. Participants and sequence are DERIVED — never authored. The first call of a path's entry step is its ROOT: `actor → Client` for a start entry, the scheduling `Client → Manager` call for a timeEvent entry, the QUEUED call into the receiving Manager for an acceptEvent entry; every later call's `from` must already have been reached on that path. Where a Manager operation rides BOTH Client surfaces, draw both and tag them with `alt` (`"s1"` = the actor→Client pair, `"s2"` = the Client→Manager pair) — alt groups are step-local and every call in a group targets the same Manager. Draw a utility call only where the verb IS that node's business work; a signal delivery is drawn as its queued Manager→Manager business edge and NEVER also as a bus call. A node the system genuinely does no work at becomes a NOTE (which carries no step) — never a fabricated call.

IDENTITY BY NAME: every component is identified by its NAME — you do NOT emit any id, and you do NOT emit a component's layer (it is fixed by its kind and the server derives it). Component names must be UNIQUE. In `relationships` and in a dynamic view's step calls, reference components by their NAME (the from/to are component names, or an actor's role name). In each dynamic view set `useCase` to that use case's NAME (exactly as it appears in the CoreUseCases context — core OR nonCore) — do NOT emit a view key; the server derives it. The server resolves every name to its internal id and rejects any name that does not match a component, an owning-use-case actor, or a use case.

## Exit criteria

- `.aiarch/state/project.json` → `.systemDesign` holds the typed `System` model, and it renders to Structurizr DSL that parses cleanly (no parser errors, no ERROR-level log lines) during server-side artifact validation.
- Every Manager/Engine/ResourceAccess `Component` records `encapsulatesVolatilities` — the exact committed volatility name(s) from `.volatilities` its `Encapsulates` prose cites, verbatim (a name matching no committed volatility is `DH-COMP-VOL-DANGLING`; an empty list on a Manager/Engine/ResourceAccess is `DH-COMP-NO-VOLATILITY`).
- Every committed volatility has a disposition: encapsulated by a component (normally one, or a ratified facet group), explicitly deferred to an operational concept (recorded on the draft, resolved in `.operationalConcepts`), or delegated to a third-party service. A volatility with none is `DH-VOL-ENCAP-MISSING`.
- Cardinality limits respected (see [[the-method-layers]]).
- **Every** use case from `.coreUseCases` — core AND every non-core variation — has a `DynamicView` that traces cleanly through the layers (founder extension; enforced by `USECASE-DYNAMIC-MISSING`). No use case ships without a call chain.
- Every realization is step-keyed and **complete**: every `action`/`timeEvent`/`acceptEvent` node of every use case carries a non-empty, ordered fragment; steps sit on no ineligible kind; the whole `CC-*` family and the retargeted `DV-*` family are clean at Error severity.
- Every human entry that both Client surfaces can perform is realized on both, as step-local `alt` groups; any single-surface entry carries a recorded product justification.
- Every node converted to a note is a node the system genuinely does no work at, and its label says so.
- The cross-Manager symmetry pass has been made once over the realized corpus, with every asymmetry either justified by name or fixed.

Move to `the-method-operational-concepts`.

## Anti-patterns to reject

- **Missing dynamic view for any use case (core or non-core variation)** — incomplete validation; rejected by `USECASE-DYNAMIC-MISSING` (founder extension).
- **An unrealized use case** — a view with no steps, or an `action`/`timeEvent`/`acceptEvent` node with no fragment (`CC-COVERAGE`). There is no "no-call" escape hatch.
- **A note conversion made to clear a finding** — the gate becoming the reason the requirements artifact changed. Convert only on the merits (the system does no work there), and say so in the label.
- **Adding, splitting, or reshaping a component so a chain draws** — functional decomposition through the validation door. STOP and escalate instead.
- **Mass-produced shallow use cases (or a real one left uncaptured) to manage the gate's cost** — the gate covers what is authored; author what the design honestly needs.
- **A fabricated call** — any call with no implementation or contract behind it, drawn to connect a chain. A dishonest realization is worse than a missing one: it passes.
- **Drawing a delivery twice** — the queued Manager→Manager edge AND a bus call for the same act.
- **Dynamic view enters multiple Managers from a Client** — Don't 6a; decomposition wrong.
- **Dynamic view shows Client → Engine, Client → ResourceAccess, or Client → Resource directly** — layer skip; decomposition wrong.
- **Engines or ResourceAccess publishing events** — Don't rule violated; component misclassified.
- **A single-surface entry with no recorded justification** where the operation rides both Client→Manager edges — R4/cross-surface drift.
- **Mermaid `flowchart` or `sequenceDiagram`** — both are deprecated for Method artifacts; use PlantUML activity (new syntax) for use-case activity diagrams. The PlantUML hook validates every block on save.
- **A component with an empty `Encapsulates` where one is required (or a feature-name in it)** — usually means it was named before the volatility was clear. The same goes for an empty `EncapsulatesVolatilities` on a Manager/Engine/ResourceAccess (`DH-COMP-NO-VOLATILITY`), or a typed entry that does not match a committed volatility name verbatim (`DH-COMP-VOL-DANGLING`).
- **An `Encapsulates` over 150 characters, or one that documents retention / persistence schema / idempotency mechanics / rationale** — that detail belongs in the `.operationalConcepts` or `.volatilities` artifact. The rendered on-element description names the encapsulated volatility and the role; nothing more.

## TradeMe reference

Re-read Ch. 5 §6 for the worked example: the architect validated 8 use cases — 7 as call chains, 1 (Terminate Tradesman) as a sequence diagram because timing mattered. Two things to take from it. First, the discipline: TradeMe demonstrated its design against **every** use case the customer provided, which is what the founder extension makes mandatory here. Second, ch. 5's own observation about the result — *"note the high degree of self-similarity or symmetry in the call chains"* — is the standard your realized corpus is held to (Step 9's symmetry pass). The timing detail that earned Terminate Tradesman its sequence diagram lives in `.operationalConcepts` on this platform; the ordering it also showed is carried by the step fragments themselves.

## Common failure modes

- **A volatility maps to two components without a ratified facet group.** A volatility is normally owned by one component (or a ratified facet group). Two independent claimants means one of them is unnecessary, or the volatility was poorly stated — resolve in the `.volatilities` artifact first, then redo Step 1.
- **A component has no volatility from `.volatilities` behind it.** Drop the component (`DH-COMP-NO-VOLATILITY`).
- **Manager-to-Engine ratio wrong (too few Engines).** Either Managers are too thick — extract Engines — or there are too few Engines because business activities are buried inside Managers. Refactor.
- **A Utility doesn't pass the cappuccino-machine test.** It is not a Utility. Reclassify (often as ResourceAccess or Engine) or remove.
- **A core use case won't draw.** The decomposition is wrong. Do not weaken the use case to fit; revise the decomposition.
