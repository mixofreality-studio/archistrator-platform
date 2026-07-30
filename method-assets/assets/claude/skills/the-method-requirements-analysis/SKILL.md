---
name: the-method-requirements-analysis
description: System Design — build the glossary via the Four Questions and derive the Required Behaviors by scrubbing solutions-masquerading-as-requirements. Architect drives both passes together. Reads the committed .mission and the research corpus from project.json. Produces the typed Glossary and Required Behaviors (each behavior carries statedAs provenance + a volatilityHint hand-off to step 4) committed to project.json → .glossary and .scrubbedRequirements (the wire kind keeps its identity; the customer-facing label is "Required Behaviors"). Invoke after [[the-method-business-alignment]], before [[the-method-volatility-identification]].
---

# Requirements Analysis (Glossary + Scrubbing)

This phase pairs two architect-driven activities that must happen together: building the glossary (so terms are stable) and scrubbing solutions-masquerading-as-requirements (which often surface during glossary work).

## Canonical source

**Primary scrubbing:** Löwy, *Righting Software*, Chapter 2 §3.3 "Solutions Masquerading as Requirements".

**Primary glossary / naming:** Chapter 3 §4.1 "What's in a Name" and §4.2 "The Four Questions".

**Worked example:** Ch. 5 "TradeMe Glossary".

**Standard reference:** Appendix C §3.1d "Eliminate solutions masquerading as requirements" (System Design Guidelines, item 1d).

## Input

State is git-as-DB: archistrator is a single Go-server repo whose canonical project state lives in `.aiarch/state/project.json` (a typed JSON aggregate). Markdown is a render-on-read of the typed state, never the source of truth.

- The committed **mission** artifact in `.aiarch/state/project.json` → `.mission` (from the prior phase)
- The research corpus in `.aiarch/state/project.json`
- The PM's customer-input notes carried in project state (if present)

## Outputs

The two typed models (Go shapes in `internal/resourceaccess/projectstate/models_phase1.go`), committed to `.aiarch/state/project.json`:

1. **`Glossary`** (`Items []GlossaryItem`, each `Term`/`Definition`/category) → `.glossary`
2. **`ScrubbedRequirements`** (container, unchanged shape; `Items []Requirement`) → `.scrubbedRequirements` — the customer-facing label is **"Required Behaviors"**. Each `Requirement` item is `{id, behavior, statedAs, volatilityHint}` (see Pass 2).

Neither is a `*.md` file — any markdown below is a render-on-read of the typed slot. Per the two usage patterns (agentic/CI dispatch and local interactive), the agent emits each typed model and commits it into its slot in `project.json`; the server stages it (`StageArtifactForReview`) for the human review gate.

## Procedure

### Pass 1 — Build the glossary (ch. 3)

Use the **Four Questions** to canvas the domain. The book's ch. 3 §4.2 set is four questions — who / what / how / where; the table below is the platform's deliberate extension, splitting "how" into business activity vs resource access. Using these classification questions as the glossary canvassing technique is itself a deliberate founder extension — the book builds its glossary (ch. 3 §4.1, ch. 5) without this framing; we keep the practice and name it as ours.

| Question | What it captures | Will later become |
|---|---|---|
| **Who** uses or interacts? | Actors, roles | Clients |
| **What** is required of the system? | Behaviors, use cases | Managers |
| **How** does it perform business activities? | Activities, algorithms | Engines |
| **How** does it access resources? | Verbs over data | ResourceAccess |
| **Where** does state live? | Stores, queues, external systems | Resources |

For each question, list every distinct domain noun or verb that appears in the research. Define each in one line using **customer language**. Each becomes a `GlossaryItem` (term + one-line definition + Four-Question category) in the typed `Glossary` committed to `.glossary`.

Rendered view (`Glossary` → render-on-read; the JSON is the source of truth):

```markdown
# Glossary

## Actors (Who)
- **Tradesman** — a skilled service provider registered with the platform.
- **Contractor** — a tradesman managing a project on behalf of a customer.
...

## Behaviors (What)
- **Match Tradesman** — assign the best-fit tradesman to a customer request.
- **Onboard Tradesman** — register and verify a new tradesman.
...

## Activities (How)
- **Skill matching** — rank tradesmen by skill alignment with request.
- **Availability check** — filter tradesmen by current schedule.
...

## Resource access (How)
- **Credit tradesman account** — atomic verb increasing balance.
- **Search tradesman registry** — atomic verb querying registered set.
...

## Resources (Where)
- **Tradesman registry** — the persisted set of registered tradesmen.
- **Project store** — the persisted state of ongoing projects.
...
```

### Pass 2 — Derive the Required Behaviors (scrub solutions-masquerading-as-requirements, ch. 2)

For each requirement statement in research, drive Löwy's interrogation:

1. **Is this a solution or a true requirement?**
2. **Are there other possible solutions?** If yes, this is a solution, not a requirement.
3. **What is the real requirement and underlying volatility?**
4. **Is the volatility itself a true requirement, or another solution?** (Recurse.)

Per ch. 2: *"Start by pointing out the solutions masquerading as requirements, and ask if there are other possible solutions? If so, then what were the real requirements and the underlying volatility? Once you identify the volatility, you must determine if the need to address that volatility is a true requirement or is still a solution masquerading as a requirement. Once you have finished scrubbing away all the solutions, what you are left with are likely great candidates for volatility-based decomposition."*

> **Functional decomposition as a discovery tool (ch. 2 §"When to use functional decomposition").** Driving a functional decomposition of the requirements to a fine level is a LEGITIMATE requirements-analysis technique: it uncovers hidden or implied functionality and exposes redundancies among the asks while you gather and scrub them. It must NEVER become the design — there is never a direct mapping from requirements to components. Use it to interrogate the asks here, then discard it; the architecture comes from volatility, not from this breakdown.

**Examples from the book to internalize:**

| Stated requirement | First scrub | Final scrub |
|---|---|---|
| "Send email" | Notify users; transport is volatile | (Final: notify users) |
| "Cooking" | Feeding | Well-being |
| "We need a queue" | User receives events | User receives events in order |
| "Add a notification service" | Notify users on state changes | Same — but architecture must encapsulate transport volatility |

The scrubbed survivors ARE the **Required Behaviors** — the typed list committed to `.scrubbedRequirements` (the wire kind keeps its identity; the customer-facing label is "Required Behaviors"). This is the last fully readable, non-technical artifact — the customer's "did the machine understand what my system must do?" checkpoint — so each behavior is one imperative sentence in business language, and the trace back to the customer's own words is preserved on the survivor.

Each item is a typed `Requirement` with four fields:

| Field | Meaning | Rule |
|---|---|---|
| `id` | stable identity — `B-01`, `B-02`, … | **contiguous — NO numeric gaps, ever.** Renumber survivors so the ids run `B-01..B-NN` with no holes. |
| `behavior` | the scrubbed behavior, imperative business language | solution-free; what the system must do, never how |
| `statedAs` | the raw stated ask(s) this was scrubbed/consolidated from (nullable string[]) | when several stated asks collapse into one behavior — or an ask is retired — ALL of their original phrasings live here. Provenance lives on the survivor, so an absorbed ask leaves an entry in `statedAs`, never a hole in the id sequence. |
| `volatilityHint` | the candidate volatility name(s) this behavior implies (nullable string[]) | the **hand-off to [[the-method-volatility-identification]]** (step 4) — the seed list step 4 consumes; every behavior that implies change MUST carry it. |

`statedAs` and `volatilityHint` are NOT optional decoration — the downstream view renders both columns and step 4 consumes `volatilityHint`. A draft that leaves them empty ships a visibly broken artifact (a known root-cause defect: the view rendered fields the prompt never filled). See Draft-job doctrine.

Rendered view of the typed `Required Behaviors` committed to `.scrubbedRequirements` (the JSON is the source of truth; this table is render-on-read):

```markdown
# Required Behaviors

| id | Behavior | Stated as (customer's words) | Volatility hint (→ [[the-method-volatility-identification]]) |
|---|---|---|---|
| B-01 | Notify the customer when an order is placed | "Send confirmation email when order placed" | Notification transport varies by customer and over time |
| B-02 | Serve read-heavy access fast | "Use Redis cache for hot data"; "keep the dashboard snappy" | Storage / caching technology may change |
| B-03 | ... | ... | ... |
```

The **volatilityHint column is critical** — it is the input to [[the-method-volatility-identification]]. Every behavior that implies change must surface a candidate volatility there; the `statedAs` column keeps the trace back to the customer's original words.

### Pass 3 — Reconcile glossary and required behaviors

Read both typed models (the staged/committed `.glossary` and `.scrubbedRequirements` slots). Check:
- Every actor a behavior names is in the glossary (under Who)
- Every behavior maps to a glossary entry (under What)
- Where glossary terms differ from research wording, the glossary wins — revise the behaviors (and their `statedAs`/`volatilityHint`) to use glossary terms before committing
- After any consolidation this pass triggers, the ids are still contiguous (`B-01..B-NN`, no gaps)

## PM role

The PM is dispatched after architect produces drafts:
- Glossary: PM flags missing terms or misnamed concepts the customer would not recognize
- Required behaviors: PM ratifies that each scrubbed behavior still serves the customer's actual need, and that `statedAs` faithfully traces back to what the customer asked for

PM does not author either model.

## Draft-job doctrine (CI dispatch)

These are the normative tasks the CI draft job (and a local `/system-design` run) executes to produce the two typed models. Each is self-contained: everything a draft agent needs to draft a sound artifact of this kind is stated here.

### Glossary

Extract the system's ubiquitous-language terms, each categorised by the classification categories (the book's Four Questions, extended): Who interacts with the system, What is required of it, How (the business activity), How (resource access), Where (state lives). Define each term crisply in business language with NO solution/implementation wording. These terms are the shared vocabulary every later artifact must reuse verbatim.

### Required behaviors

Scrub every solution out of the requirements and emit the underlying behaviors only. A behavior states what the business requires; a solution states how to build it — strip the how. "Users log in with OAuth" is a solution; "the system authenticates users" is the behavior. Emit each survivor as a typed `Requirement`:

- `id` — contiguous `B-01..B-NN`, NO gaps. When you consolidate several stated asks into one behavior, or retire an ask, renumber so the ids stay hole-free.
- `behavior` — one solution-free imperative sentence in business language, traceable to the mission.
- `statedAs` — **MUST be populated**: the raw customer ask(s) this behavior was scrubbed from (every phrasing you consolidated). This is the provenance that lets a retired/absorbed ask survive on the survivor instead of leaving an id gap.
- `volatilityHint` — **MUST be populated** for every behavior that implies change: the candidate volatility name(s) the behavior implies. This is the seed list [[the-method-volatility-identification]] consumes; leaving it empty breaks the step-4 hand-off (a known root-cause defect — the view renders this column, so an empty draft ships a visibly broken artifact).

A draft that emits behaviors with empty `statedAs`, or empty `volatilityHint` where change is implied, is incomplete and will fail review.

## Exit criteria (for router)

`.aiarch/state/project.json` → `.glossary` and `.scrubbedRequirements` both hold their typed models. Glossary has entries under every classification category (Who / What / How-activity / How-resource-access / Where). `.scrubbedRequirements` holds the Required Behaviors with **contiguous** `B-NN` ids (no gaps), every behavior carrying `statedAs` provenance and — where change is implied — a `volatilityHint`. Every research ask is accounted for: either as a survivor behavior or folded into a survivor's `statedAs`. Move to `the-method-volatility-identification`.

## Anti-patterns to reject

- **CRUD-style entries** in the glossary ("create order", "update user") — these are implementations, not behaviors. Restate as business verbs.
- **Untouched behaviors** in `.scrubbedRequirements` — if a `behavior` matches its `statedAs` verbatim, you didn't interrogate hard enough.
- **Empty `statedAs` / `volatilityHint`** — a behavior with no recorded stated-ask provenance, or (where change is implied) no candidate volatility, is an incomplete draft; the view renders both columns and step 4 consumes the hint.
- **Numeric id gaps** — `B-03` then `B-05` is never acceptable; renumber survivors contiguously. Retired/absorbed asks leave a `statedAs` entry on the survivor, not a hole in the id sequence.
- **Marketing names** in glossary — replace with operational terms.
- **Tech-stack names** anywhere — "Redis cache" is not a behavior, "fast read access" is.
