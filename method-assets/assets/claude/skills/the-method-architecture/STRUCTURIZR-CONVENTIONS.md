# Structurizr conventions for The Method

How to encode Löwy's volatility-based decomposition in Structurizr DSL.

## Where the DSL lives

State is git-as-DB: the architecture is a **typed entry in
`.aiarch/state/project.json` under `.systemDesign`** — git is the database.
Structurizr DSL is a **render-on-read** of that typed model, never the source of
truth. There is no `designs/<product>/system/architecture.dsl` file (that was a
methodpoc artifact); when you "write DSL" you are shaping the typed
`.systemDesign` slot, and the DSL is produced by rendering it.

The syntax conventions below remain canonical — they describe the shape of the
DSL rendered from `.systemDesign`, and the typed model carries the same
elements, relationships, descriptions, and views.

## Element model

The Method's layers map to Structurizr **containers** inside a single
**software system**, tagged by layer. Use `tags` to drive styling and
validation.

```
clients          → tag "client"
managers         → tag "manager"
engines          → tag "engine"
resourceAccess   → tag "resource-access"
resources        → tag "resource"
utilities (bar)  → tag "utility"
```

## Description style

Element descriptions (the second quoted string on a `component` / `container` /
`softwareSystem` line) must be **≤ 150 characters**. Keep them short: name what
the element **encapsulates**, then a brief verb-phrase for what it does.

The component description is metadata in the diagram — not the place to
document edge cases, retention policy, persistence schema, or rationale.
That detail belongs in:

- comment blocks above the element in the rendered DSL,
- the committed **operational-concepts** artifact (the `.operationalConcepts`
  slot — runtime behavior, idempotency, retries),
- the committed **volatilities** artifact (the `.volatilities` slot — the *why*
  of the encapsulation).

**Pattern by layer:**

| Layer | Template |
|---|---|
| Manager | `Encapsulates <Workflow> volatility. <one-line role>.` |
| Engine | `Encapsulates <Policy/Model> volatility. <one-line computation>.` |
| ResourceAccess | `Atomic verbs over <Resource>: <verb1>, <verb2>, <verb3>.` *(or)* `Encapsulates <Resource> volatility. <one-line role>.` |
| Resource | `<storage kind / external system> for <purpose>. <one durability/scope note>.` |
| Client | `<transport> for <actor/origin>. Routes to <Manager>.` |

❌ Avoid (over 150, mixes responsibility with implementation detail):

```dsl
settlementEngine = component "Settlement Engine" "Encapsulates RevenueShareTerm + ComputeCostPricing + SettlementSchedule + BillingTerms volatility: given a cycle's revenue events, compute events, and the customer's terms, produces the signed net amount and routes payout/charge. Multi-purpose by intent — split candidate flagged in volatilities.md if it grows." "kotlin" "engine"
```

✅ Preferred:

```dsl
settlementEngine = component "Settlement Engine" "Encapsulates RevenueShareTerm + ComputeCostPricing + SettlementSchedule + BillingTerms volatility. Computes signed net and payout/charge routing." "kotlin" "engine"
```

## Edge-label conventions

Relationship labels name **what** the caller invokes, in the **vocabulary of
the destination layer's responsibility**. Architecture diagrams are infrastructure-
agnostic — never put workflow-engine primitives (`Activity:`,
`StartWorkflow(...)`, `SignalExternalWorkflow`, `Await Signal`, etc.) in
the labels. Those belong in the committed operational-concepts artifact
(the `.operationalConcepts` slot).

View and edge labels are lowercase-first call-syntax prose (`submitOrder(orderId)`)
while contract operation names are PascalCase code surface (`SubmitOrder`); the
checkers case-fold between the two, so a lowercase-first label and its PascalCase
contract op are the same name and never need to match byte-for-byte.

| Edge | Label shape | Why |
|---|---|---|
| Client → Manager | `<managerMethodName>(<args>) → <result>` (or just `<managerMethodName>`) | The Client invokes a method on the Manager. The label IS the method name. List alternative methods on the same edge with `\|`. |
| Manager → Engine | `<EngineMethodName>(<args>) → <output>` | Engines are pure computation. The label is the engine method signature. |
| Manager → ResourceAccess | `<atomicBusinessVerb>(<noun>)` (e.g., `appendEvent(OrderSubmitted)`, `readProjection(projectId)`) | ResourceAccess exposes atomic *business* verbs, not CRUD and not workflow primitives. The label names the verb. Multiple verbs on one edge: separate with `/`. |
| ResourceAccess → Resource | resource-domain verb + idempotency note (e.g., `append entry / read range (idempotency: UNIQUE(event_id))`) | Use verbs that name the *effect* at the resource boundary, not platform-specific commands. If swapping the platform (Argo→Flux, Postgres→DynamoDB, Stripe→Adyen) would force a label change, the label is too implementation-specific — pull the platform name out and rename to the generic effect. |
| Manager → Manager (queued) | `delivers <SignalName> (queued)` | The closed-layering queued-sideways exception. Label names the business signal; the delivery infrastructure is operational detail. |
| anyone → Utility | `<verb>` (e.g., `Logs`, `AuthN/AuthZ`) | Cross-cutting; one-word verbs suffice. |

❌ Avoid (leaks infrastructure or platform names into labels):

```
webClient -> orderManager "StartWorkflow(SubmitOrderWorkflow, orderId, SubmitOrderRequest)"
orderManager -> orderAccess "Activity: AppendEvent(OrderSubmitted) [StartToClose=15m]"
orderManager -> pricingEngine "Activity: ComputeTotal(lineItems) → total"
operatedRuntimeAccess -> operatedRuntime "git commit manifests; ArgoCD reconciles"
merchantGatewayAccess -> merchantGateway "POST /charges (Stripe Idempotency-Key header)"
```

✅ Preferred (vocabulary of the destination layer; platform-agnostic):

```
webClient -> orderManager "submitOrder(orderId, lineItems)"
orderManager -> orderAccess "appendEvent(OrderSubmitted)"
orderManager -> pricingEngine "ComputeTotal(lineItems) → total"
operatedRuntimeAccess -> operatedRuntime "publish desired state (idempotency: deterministic manifest paths)"
merchantGatewayAccess -> merchantGateway "charge customer (idempotency: gateway event id)"
```

**Rule of thumb.** If you swapped Temporal for Akka, Argo for Flux,
Postgres for DynamoDB, or Stripe for Adyen, would the label have to
change? If yes, the label is leaking implementation detail through the
encapsulation — pull the platform name out and use the generic effect.
Infrastructure-specific primitives, retry/timeout policies, and platform API
shapes belong in the committed operational-concepts artifact (the
`.operationalConcepts` slot), not in Structurizr labels.

## Template

This is the starting template that `/system-design` writes. Names, layers,
and call chains get filled in per product.

```dsl
workspace "<Product Name>" "Volatility-based decomposition per The Method." {

    !identifiers hierarchical

    model {
        // ---- Actors ----
        user = person "User" "Primary customer / system user."

        // ---- The System ----
        system = softwareSystem "<Product Name>" "<one-line mission>" {

            // ===== Clients =====
            // Entry points. UI / public API.
            webApp = container "Web App" "User-facing web client." "react" "client"

            // ===== Managers =====
            // Workflow orchestration. Encapsulates use-case volatility.
            // Managers are "almost expendable" — they hold the volatile glue.
            // <ManagerName>Manager = container "<Manager Name>" "Encapsulates <volatility>." "" "manager"

            // ===== Engines =====
            // Pure business activities. No I/O.
            // <EngineName>Engine = container "<Engine Name>" "Encapsulates <volatility>." "" "engine"

            // ===== ResourceAccess =====
            // Atomic business verbs against resources. Resource-neutral interface.
            // <Access>Access = container "<Access> Access" "Atomic verbs over <Resource>." "" "resource-access"

            // ===== Resources =====
            // Data / external systems.
            // <Resource>Db = container "<Resource> DB" "" "postgres" "resource"

            // ===== Utilities bar =====
            // Cross-cutting. Anyone can call.
            logging  = container "Logging"  "Structured logs."     "" "utility"
            security = container "Security" "AuthN/AuthZ."          "" "utility"
            diagnostics = container "Diagnostics" "Health/metrics." "" "utility"
        }

        // ---- Relationships ----
        // Only the rules from The Method are allowed:
        //   Client → one Manager per use case
        //   Manager → Engine | ResourceAccess | (queued) Manager
        //   Engine → ResourceAccess
        //   ResourceAccess → Resource
        //   anyone → Utility
        //
        // Define the relationships needed to support every core use case.
    }

    views {
        // -------- Static architecture (the layered pyramid) --------
        container system "static-architecture" "Layered static architecture." {
            include *
            autolayout tb
        }

        // -------- One dynamic view per USE CASE = a call chain --------
        // NOTE: this flat edge list is the RENDER. You author the realization
        // step-keyed (one ordered call fragment per activity node); the
        // renderer linearizes the fragments in activity-graph order to emit
        // the block below. Never author a linearization by hand.
        // dynamic system "<use-case-key>" "<Use Case Name>" {
        //     user -> system.webApp "<actor action>"
        //     system.webApp -> system.<manager> "<API call>"
        //     system.<manager> -> system.<engine> "<method>"
        //     system.<manager> -> system.<access> "<verb>"
        //     system.<access> -> system.<resource> "<I/O>"
        //     autolayout lr
        // }

        styles {
            element "Person" {
                shape Person
                color "#ffffff"
                background "#1b5e20"
            }
            element "client" {
                background "#2e7d32"
                color "#ffffff"
            }
            element "manager" {
                background "#1565c0"
                color "#ffffff"
            }
            element "engine" {
                background "#6a1b9a"
                color "#ffffff"
            }
            element "resource-access" {
                background "#ef6c00"
                color "#ffffff"
            }
            element "resource" {
                background "#424242"
                color "#ffffff"
                shape Cylinder
            }
            element "utility" {
                background "#546e7a"
                color "#ffffff"
                shape RoundedBox
            }
        }
    }
}
```

## Validation rules (used by `/system-design`)

After the DSL is written, the system-architect agent validates it against The
Method's rules. Each rule below is an automated check.

| Rule | Check |
|---|---|
| Every use case has a dynamic view, and every view is realized | One view per use case in the committed set — core AND every nonCore variation (founder extension) — each carrying a call fragment for every `action`/`timeEvent`/`acceptEvent` node of that use case's activity diagram |
| Clients call exactly one Manager per use case | In each dynamic view, count of distinct Manager targets ≤ 1 |
| No calling up | No relationship goes from a lower layer to a higher layer |
| No calling sideways within a layer | Except queued Manager→Manager (model as `delivers <SignalName> (queued)`) |
| No skipping layers | Client doesn't call Engine/ResourceAccess/Resource directly |
| Engines/ResourceAccess/Resources don't subscribe | No incoming queued edges |
| Cardinality | ≤5 Managers (no subsystems); ≤3 per subsystem; golden Engines-to-Managers ratio — fewer Engines than Managers |
| Total component count | Order of magnitude 10 |
| Edge-label vocabulary | Labels use the destination layer's vocabulary: Client→Manager = manager method name; Manager→Engine = engine method signature; Manager→ResourceAccess = atomic business verbs; ResourceAccess→Resource = resource-native I/O. No workflow-engine primitives in labels (no `Activity:`, no `StartWorkflow(`, etc.). See "Edge-label conventions" above. |
| Execution substrate stays out of every view | A Manager's own durable primitives (timers, awaited signals, child executions) are how it RUNS, not calls between components — they are not edges anywhere. The only cross-component messaging verbs live on the `MessageBus` utility, and a delivery already carried by a queued Manager→Manager edge is drawn as that edge and never also as a bus call. See "Execution substrate, messaging utilities, and what a call chain draws" below. |

## Rendering

The DSL is **rendered on read** from the typed `.systemDesign` slot. To view it
locally, render the slot to a Structurizr workspace and open it in the current
`structurizr/structurizr` Docker image (NOT the deprecated `structurizr/lite`).
The render emits a single `workspace.dsl`; there is no separate
`architecture.dsl` artifact to keep in sync (that drift problem belonged to the
old methodpoc two-file layout and does not exist when the DSL is a render of one
typed model).

## Validation

Validation happens against the **typed `.systemDesign` model**, not a `.dsl`
file in a designs tree. Two layers of checking:

1. **The Method rules** — the layer/call-direction/cardinality/edge-label rules
   below are validated structurally over the typed model (see "Validation rules"
   above). These do not require a Structurizr parser.

2. **DSL parse validity** — whenever `.systemDesign` is rendered to Structurizr
   DSL, the rendered workspace MUST parse cleanly under
   `structurizr/structurizr validate`. Structurizr is strict and the parser
   errors are not always obvious from reading the DSL (see "Common DSL pitfalls"
   below). Treat any `ERROR` line in the parser output as a failure — the bare
   validate command exits 0 on certain syntax errors (notably `styles` block
   syntax) even though the server cannot load the workspace. The renderer is
   responsible for emitting DSL that parses; the pitfalls section encodes the
   traps so the rendered output avoids them.

**Exit criteria for any architecture skill that shapes `.systemDesign`**: the
typed model passes The Method rules, and its DSL rendering parses cleanly under
`structurizr/structurizr validate`. Do not move on with a model that fails
either check.

## Common DSL pitfalls

The Structurizr DSL parser has several traps that bite agents. Encode all of
these by following the template above.

**Pitfall: inline-brace `element` blocks in `styles`.** The new
`structurizr/structurizr` parser rejects inline-brace form for `element`.
Each property must be on its own line.

❌ Rejected (server fails to load, `validate` quietly returns 0):

```dsl
styles {
    element "Person" { shape Person color "#ffffff" background "#1b5e20" }
}
```

✅ Required:

```dsl
styles {
    element "Person" {
        shape Person
        color "#ffffff"
        background "#1b5e20"
    }
}
```

**Pitfall: dynamic-view edges that don't exist in the model.** Every
relationship used inside a `dynamic` view MUST already be declared in the
`model` block. The parser does NOT auto-create edges from dynamic views.

❌ Parser error: `A relationship between <X> and <Y> does not exist in model`.

The fix is always to add the missing `<X> -> <Y> "..."` line under the
`// Manager → ResourceAccess` (or appropriate) section of the model, not to
remove the dynamic-view edge.

**Pitfall: editing the rendered DSL instead of the model.** The DSL is a
render-on-read of the typed `.systemDesign` slot. Hand-editing the rendered
`workspace.dsl` does not change the model — the next render overwrites your
edit and the change is lost. Always change the typed `.systemDesign` slot and
re-render; the DSL is an output, never the source of truth.

**Pitfall: escaped quotes inside relationship descriptions.** The parser
does NOT support `\"` inside `"..."` relationship-description strings.
If a label needs to wrap a name in quotes, use square brackets instead —
e.g., `deliver[orderSubmitted]`, not `deliver \"orderSubmitted\"`.

## Why dynamic views = call chains

The book (ch. 4) defines a call chain as "a sequence-style diagram per core
use case showing the chain through Client → Manager → Engine/ResourceAccess
→ Resource." Structurizr's dynamic views are exactly that primitive: an
ordered list of relationships between containers, scoped to a single use
case. The dynamic view IS the call chain.

## Execution substrate, messaging utilities, and what a call chain draws

The execution substrate a Manager's use-case methods run *on* — a
durable-workflow engine, actor cluster, or scheduler runtime — is **not a
component in the architecture at all**. A Manager's own durable primitives
(timers, awaited signals, child executions, continue-as-new) are how that
Manager RUNS, not calls from one component to another, so they appear in no
view, static or dynamic.

**Why.** Löwy never draws the workflow engine inside a TradeMe call chain
(ch. 5). Drawing it would repeat the same edges across every use case (every
Manager has the same primitives), obscure the business flow with
infrastructure vocabulary, and treat an implementation choice as if it were
part of the use case's domain semantics — it isn't.

**What IS a component: the messaging utility.** Exactly two verbs cross a
component boundary — delivering a queued message to another Manager, and
registering a recurring schedule — and those live on a `MessageBus`
**Utility** with a restricted clientele (Managers only, ch. 5:
*"you should disallow Client-to-Client communication across the bus"* made
executable). It encapsulates the substrate-choice volatility, so no Manager
binds to a runtime API. It is a declared component with declared
`Manager → MessageBus` relationships, like `Security` or `Logging`.

**Verb calls draw; deliveries don't.** In a realization:

- a bus call **is** drawn — a real, numbered, tintable edge — where the verb
  IS that activity node's business work (registering a customer's recurring
  billing schedule on the onboarding chain). The no-lines-to-the-utilities-bar
  convention of the static diagram does not apply in a call chain, where the
  call IS the content;
- a **delivery** whose business meaning is already a declared queued
  Manager→Manager relationship is realized as **that queued call and never
  additionally as a `deliverSignal` bus call**. The bus is the medium of the
  queued edge, not a party to it; drawing both double-counts one act;
- boot-time wiring that no use-case flow performs (schedules registered at
  startup) is drawn nowhere. Its relationship stays declared and is exempt
  from relationship coverage as an ambient dependency — an exemption the
  checker reports on an Info line rather than applying silently.

**Where the runtime detail goes instead.** Infrastructure-specific primitive
names and their retention/replay semantics — Temporal `Activity` / `Signal` /
`Schedule`, Akka actor messages — belong in the committed operational-concepts
artifact (the `.operationalConcepts` slot), never in an architecture label.

**What a call chain draws.** The business-logical calls only:

- `actor → Client` — the human's gesture (sync; actors touch Clients and
  nothing else).
- `Client → Manager` — the use case starts or resumes; the label is the
  manager method name.
- `Manager → Engine` — the engine method signature.
- `Manager → ResourceAccess` — the atomic business verb that does the I/O.
- `ResourceAccess → Resource` — the actual I/O.
- `Manager → Manager` (queued) — a cross-Manager delivery, drawn directly per
  the closed-layer queued-sideways carve-out (ch. 3). The label names the
  business signal — e.g. `delivers applyDelinquencyPolicy (queued)`.
- `Manager → Utility` — where the utility verb is the step's own business
  work (see above).

**Suspend-points.** A `Phase A: workflow suspends → Phase B: client signals
it` interaction is conveyed by fragment order alone: the resuming
`actor → Client → Manager` fragment sits on the activity node that receives
the human's action, after the Manager's last pre-suspend verb. No
await-signal edge is drawn — the substrate has no edges.

**Validation impact.** Every component→component call in a realization must
match a declared relationship on `(from, to, mode)`, so a drawn bus verb
requires its `Manager → MessageBus` relationship to exist; and a delivery
drawn twice shows up as an unexplained extra edge in review, not as a rule
failure — that one is the critique's job.
