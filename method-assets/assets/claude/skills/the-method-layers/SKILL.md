---
name: the-method-layers
description: Reference for the layer model in Juval Löwy's The Method — Clients, Managers, Engines, ResourceAccess, Resources, Utilities — plus the interaction rules, interaction don'ts, and cardinality limits. Use when another skill needs the authoritative classification, naming, or call-graph rule for a component.
---

# The Method — Layers

This is a pure reference skill. It holds the canonical layer model, interaction rules, interaction don'ts, and cardinality limits. Other skills (decomposition, architecture, design-standard-check, validate-architecture) cite this skill instead of restating the rules inline.

Sources:
- Ch. 3 §3 "Typical Layers" — what each layer encapsulates.
- Ch. 3 §4 "Classification Guidelines" — naming, Four Questions, Managers-to-Engines ratio, key observations.
- Ch. 3 §5 "Subsystems and Services" — every component as a service; internal vs external communication.
- Ch. 3 §6 "Open and Closed Architectures" — layering rules and design "don'ts".
- Ch. 4 §2 "Composable Design" — the smallest-set principle.
- Appendix C §3 "System Design Guidelines" — items 2 (Cardinality), 4 (Layers), 5 (Interaction rules), 6 (Interaction don'ts).

## The six layers

*The Method* uses five horizontal layers (Clients, Managers, Engines, ResourceAccess, Resources — top to bottom) plus a Utilities bar that runs alongside them. Every component lives in exactly one layer.

| Layer | What goes here | Naming | Examples |
|---|---|---|---|
| **Clients** | Entry points: end-user apps, public APIs, other systems. Encapsulates client volatility (web/mobile/desktop/agent etc.). | Form-descriptive: `WebApp`, `MobileApp`, `PublicAPI`. | React app, iOS app, REST gateway |
| **Managers** | Workflow / sequence volatility for a *family* of related use cases. Almost expendable — orchestrate Engines + ResourceAccess. | `<Noun>Manager` — `OrderManager`, `AccountManager`. Noun describes the encapsulated workflow volatility. | OrderManager, OnboardingManager |
| **Engines** | Business **activity** volatility — Strategy pattern. **No I/O.** Pure computation, decision, transformation. | `<Gerund>Engine` — `PricingEngine`, `MatchingEngine`, `CalculatingEngine`. Gerunds MANDATORY here and FORBIDDEN elsewhere in the business / access layers. | PricingEngine, MatchingEngine |
| **ResourceAccess** | Atomic business verbs over a Resource — `credit`, `debit`, `match`, `assign`. **Never CRUD. Never raw I/O.** Banned contract-operation shapes: `Select`/`Insert`/`Update`/`Delete` (betray a database) and `Open`/`Close`/`Seek`/`Read`/`Write` (betray a file) — such names couple every consumer to the resource; expose atomic business verbs instead. May serve more than one Resource. | `<Noun>Access` — `OrderAccess`, `AccountAccess`. Noun describes the data or business concept exposed. | OrderAccess, IdentityAccess |
| **Resources** | Physical stores, queues, external systems. Internal or external to the system. | `<Noun><Technology>` — `OrderDB`, `EmailProvider`, `EventBus`. | PostgreSQL, S3, Stripe, Kafka topic |
| **Utilities** (bar) | Cross-cutting infrastructure. Cappuccino-machine test: *"could this plausibly be used in any other system?"* If no, it is not a Utility. | Concern-descriptive: `Logging`, `Security`, `Diagnostics`, `Pub/Sub`. | Logging, Security, Pub/Sub |

### The Four Questions (Ch. 3 §4.2)

The book's set is four questions — who / what / how / where. The table below is the platform's deliberate extension: "how" is split into business activity vs resource access, and a Utilities row is added.

Loose mapping that initiates and validates classification:

| Question | Layer |
|---|---|
| Who interacts with the system? | Clients |
| What is required of the system? | Managers |
| How (business activity)? | Engines |
| How (resource access)? | ResourceAccess |
| Where (state)? | Resources |
| Cross-cutting concern usable in any system? | Utilities |

The mapping is loose because **volatility trumps everything**: if there is little or no volatility in the "how", the Manager can perform both "what" and "how".

## Interaction rules

From Appendix C §3.5 "Interaction rules". These are permissive: they describe what *can* happen.

- **Clients** call exactly **one Manager per use case** (§3.5; restated as §3.6 don't 6a — "Clients do not call multiple Managers in the same use case").
- All components can call **Utilities**.
- **Managers** and **Engines** can call **ResourceAccess**.
- **ResourceAccess** components call **Resources**.
- **Managers** can call **Engines**.
- **Managers** can queue calls to another **Manager**.

From the closed-architecture rules (App C §3.4): no calling up, no calling sideways within a layer (except queued Manager → Manager), no skipping layers.

## Interaction don'ts

From Appendix C §3.6 "Interaction don'ts" and Ch. 3 §6.5 "Design 'Don'ts'". These are prohibitive: any one of them is a red flag for functional decomposition.

- Clients do not call multiple Managers in the same use case.
- Clients do not call Engines.
- Managers do not queue calls to more than one Manager in the same use case (use Pub/Sub instead).
- Engines do not receive queued calls.
- ResourceAccess components do not receive queued calls.
- Clients do not publish events.
- Engines do not publish events.
- ResourceAccess components do not publish events.
- Resources do not publish events.
- Engines, ResourceAccess, and Resources do not subscribe to events.
- Engines never call each other.
- ResourceAccess components never call each other (use a single ResourceAccess that joins multiple Resources instead).

When you find yourself wanting to violate one of these, the underlying problem is almost always: you have a functional decomposition hiding behind Method-style names. Restart the decomposition.

## Cardinality

From Appendix C §3.2 "Cardinality" and Ch. 3 §4.3 "Managers-to-Engines Ratio".

- ≤ **5 Managers** in a system without subsystems.
- ≤ a handful of **subsystems** (rule of thumb: ≤5).
- ≤ **3 Managers per subsystem**.
- Strive for the **golden Engines-to-Managers ratio** — typically **fewer** Engines than Managers (the book: you end up with fewer Engines than you imagine):

| Managers | Typical Engines |
|---|---|
| 1 | 0–1 |
| 2 | 1 |
| 3 | 2 |
| 4 | 2–3 |
| 5 | 3 |

(Table is illustrative — the rule of thumb is fewer Engines than Managers, not the exact counts.)

- ResourceAccess components may serve more than one Resource.
- A single Manager may support **more than one family of use cases**, expressed as separate service contracts (facets) on the one Manager — a legitimate way to reduce the number of Managers in a system.
- **Order of magnitude: ~10 components total** across all layers (Ch. 4 §2 "The Architect's Mission" — "a dozen or two at the most"). If you have hundreds, the decomposition is wrong.

**Hard fail:** ≥8 Managers (violates [Directive 1 "Avoid functional decomposition"](../the-method-doctrine/SKILL.md)). Per Ch. 3 §4.3: *"you have already failed to produce a good design"* — that many Managers indicates functional or domain decomposition. Restart.

## Temporal mapping (when Managers run on Temporal)

When the operational concepts commit to Temporal as the Manager-execution infrastructure (see [[the-method-operational-concepts]] Step 1), each interaction rule above maps to a specific Temporal primitive. **This table is a CONSTRUCTION mapping, not a labelling vocabulary (reframed 2026-08-01 — it previously declared these primitives to be the edge-label form, which contradicts the label rules it points at).** The primitive names below belong in code and in the committed `.operationalConcepts` artifact; they must NEVER appear in an architecture edge label or a realization call label, which use the destination layer's own vocabulary (see [[the-method-architecture]] Step 7 and `STRUCTURIZR-CONVENTIONS.md`). The substrate is how a Manager RUNS, not a component it calls: `Activity:`, `StartWorkflow(`, `SignalExternalWorkflow` in a label is a leak, and a Manager's own timers, awaits, and child workflows are edges nowhere.

**Temporal lives only in the Manager layer.** Engines, ResourceAccess, Resources, and Utilities import no Temporal and contain no Temporal types. The mapping below is written from the Manager's perspective: when a Manager workflow needs to make a ResourceAccess call (which does I/O), the **Manager defines and registers a Temporal Activity** whose body delegates to the plain ResourceAccess method — the Activity, its `RetryPolicy`, and its timeouts all belong to the Manager. Engine calls are deterministic, so the Manager invokes them directly from workflow code with no Activity. A ResourceAccess or Engine package that imports Temporal is a layer violation.

| Method interaction | Temporal primitive (construction) | Model edge + its label vocabulary |
|---|---|---|
| Client → Manager (start a use case) | Start a Workflow | `Client → Manager` sync — `<managerMethodName>(<args>) → <result>` |
| Client → Manager (deliver a decision to a running workflow) | Signal a Workflow | same edge — the manager method that receives the decision |
| Client → Manager (read state without mutation) | Query a Workflow | same edge — the manager read method |
| Client → Manager (synchronous tracked write) | Update a Workflow | same edge — the manager write method |
| SchedulerClient → Manager (recurring) | Temporal Schedule, registered via `messageBus.registerSchedule` | `SchedulerClient → Manager` sync — the manager method the schedule drives. This edge is what a `timeEvent` entry roots on |
| Manager → Engine | Deterministic in-workflow call (NOT an Activity); Engine imports no Temporal | `Manager → Engine` sync — `<EngineMethodName>(<args>) → <output>` |
| Manager → ResourceAccess | Activity **defined and registered by the Manager**, wrapping the plain RA method; RA imports no Temporal | `Manager → ResourceAccess` sync — the **atomic business verb**, never `Activity: …` |
| Manager → Manager (queued, cross-Manager) | Signal an external Workflow, delivered via `messageBus.deliverSignal` | **A declared queued `Manager → Manager` edge** — `delivers <SignalName> (queued)`. It is a real static relationship (see below) and is what an `acceptEvent` entry roots on |
| Manager → MessageBus | The two cross-component substrate verbs | `Manager → MessageBus` sync — `deliverSignal(<SignalName>)` / `registerSchedule(<name>)`. Managers only (restricted clientele) |
| Manager → self (sleep / await / child workflow / continue-as-new) | Durable Timer, Await Signal, ExecuteChildWorkflow, Continue-As-New | **No edge at all** — execution substrate, not architecture. A suspension is conveyed by where the resuming fragment sits |
| ResourceAccess → Resource | The plain RA method's actual I/O (running inside the Manager's Activity wrapper, but itself Temporal-free) | `ResourceAccess → Resource` sync — `<atomic verb>` plus idempotency-key derivation (the Manager passes the key in, e.g. `${workflowId}:${activityId}`, since the RA method cannot read Temporal context) |

**Queued M→M IS a static edge (corrected 2026-08-01).** The book's queued sideways carve-out is modelled as what it is: a **declared queued `Manager → Manager` relationship**, named for the business signal it carries. Temporal's `SignalExternalWorkflow` is how that edge is *delivered*, and the delivery rides the `MessageBus` utility's `deliverSignal` verb — the bus is the medium of the edge, not a party to it. So the realization draws the **queued M→M call and never additionally a bus call** for the same delivery; the substrate never appears as a ResourceAccess in the model. (Earlier doctrine routed this through a `workflowExecutionAccess` ResourceAccess and denied the static edge — that is retired: it hid a real architectural relationship behind infrastructure, and left message-triggered use cases with no edge for their chain to root on.) The cardinality limit still applies: Don't 6b — a Manager does not queue to more than one Manager in the same use case.

## Layering style — prefer closed

From Appendix C §3.4 "Layers" and Ch. 3 §6.

- Prefer **closed architecture**: a component may only call into the next layer down.
- Avoid open and semi-closed/semi-open architectures.
- Resolve apparent need to "open" the architecture by **queued calls** or **asynchronous event publishing** via a Pub/Sub Utility.
- Extend the system by adding subsystems, not by breaking layering rules.

### Internal vs external communication (Ch. 3 §5)

- **Never use the same communication mechanism internally and externally.** External protocols are low-bandwidth and decoupled by design — that is their job; internal calls between components need fast, reliable channels.
- Every Manager, Engine, and ResourceAccess component is a service in its own right — a "microservice" is not a subsystem.
- The platform binds this principle once for every system built here: Temporal carries the internal mechanism, per [[platform-runtime-doctrine]] — "no synchronous HTTP between Managers".

## Key observations (Ch. 3 §4.3)

A well-designed Method system exhibits these qualities. Deviation is a smell:

| Observation | Check |
|---|---|
| Volatility decreases top-down | Clients are most volatile; Resources least. If a Resource is your most volatile component, reconsider. |
| Reuse increases top-down | Clients are hardly reusable; Utilities are universally reusable. If a Utility is single-use, it is probably not a Utility. |
| Managers are almost expendable | Each Manager's loss should leave Engines/ResourceAccess/Resources/Utilities reusable. If not, the Manager is too thick (likely functional decomposition). |
| Engines are reused across Managers | If two Managers use two different Engines to perform the same activity, you either have functional decomposition or you missed an activity volatility. Engines are designed for reuse across Managers. |
| Design is symmetric | Similar Managers and Engines designed similarly. If three of four use cases in a Manager publish events and the fourth does not, the asymmetry is a design smell. |

**The expendability trichotomy.** "Almost expendable" is a calibrated midpoint — judge each Manager by how a required change to it feels:

- **Expensive** — you fight or fear the change: the Manager is too big; functional decomposition.
- **Expendable** — you shrug the change off: the Manager is a pass-through that exists only to satisfy these guidelines; also a design flaw.
- **Almost expendable** — the change makes you think through the adaptations, perhaps estimate the work: the target.

## How to cite

When another skill needs an interaction rule or a cardinality limit, link to this file and the specific subsection. For example:

> Per [the-method-layers](../the-method-layers/SKILL.md), Engines never receive queued calls (App C §3.6).

Do not restate the layer table or the don'ts inline in other skills. If you find yourself rewriting the interaction don'ts in a third skill, link here instead.

## See also

- [the-method-doctrine](../the-method-doctrine/SKILL.md) — the Prime Directive and 9 directives, of which Directives 1–4 are operationalised by this layer model.
- [the-method-architecture](../the-method-architecture/SKILL.md) — the procedure for binning volatilities into these layers and expressing them as Structurizr DSL.
- [the-method-operational-concepts](../the-method-operational-concepts/SKILL.md) — where the infrastructure decision (Temporal vs plain) is made.
- [the-method-system-design-standard-check](../the-method-system-design-standard-check/SKILL.md) — the full Appendix C checklist applied as a quality gate.
