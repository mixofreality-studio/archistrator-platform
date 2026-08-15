---
name: the-method-compressed-solution
description: Project Design — design the compressed solution. Shorter duration via parallel work first, top resources second. Target ≤30% compression. Stops at death zone. Reads the committed normalSolution, network, planningAssumptions artifacts in project.json. Produces the typed CompressedSolution committed to project.json → .compressedSolution. Invoke after [[the-method-subcritical-solution]], before [[the-method-risk-modeling]].
---

# Compressed Solution

The compressed solution offers management a "go faster" option. It costs more (parallel work or top resources are not free). It is riskier (parallel work increases coordination overhead and execution risk). But it shortens delivery — sometimes by enough to justify the trade.

Rules from the book: parallel work *first*, top resources *second*, never below the death zone, never beyond 30% compression, never above 25% efficiency (Σ effort ÷ total cost — see below).

## Canonical source

**Primary:**
- Löwy, Ch. 7 "Project Efficiency" — the efficiency formula (§ below), effort vs. cost
- Ch. 9 §1 "Accelerating Software Projects"
- Ch. 9 §2 "Schedule Compression"
- Ch. 9 §3 "Time-Cost Curve"
- Ch. 9 §4 "Avoiding Classic Mistakes" — death zone
- Ch. 9 §6 "Network Compression"
- Ch. 11 §4 "Network Compression" — worked example with iterations

**Standard reference:** Appendix C §4.6 "Time and cost" — items 6a–6g.

## Project efficiency (Löwy ch. 7 — get this right, it gates everything below)

**Efficiency is a ratio of effort to cost, never a ratio of cost components, and never in currency.**

> "The efficiency of a project is the ratio between the sum of effort across all activities (assuming perfect utilization of people) and the actual project cost. For example, if the sum of effort across all activities is 10 man-months (assuming 30 workdays in a month), and the project cost is 50 man-months (of regular workdays), then the project efficiency is 20%." — Löwy, ch. 7, "Project Efficiency"

Formally:

```
efficiency = (Σ effort across all activities) / (actual project cost)
           = (Σ effort across all activities) / (staffing × duration)
```

Both terms are in the **same units — work-days, man-months, or man-years — never currency**:

> "Since cost is defined as staffing multiplied by time, the units of cost should be effort and time, such as man-month or man-year. It is better to use these units as opposed to currency to neutralize differences in salary, local currencies and budgets." — Löwy, ch. 7

Do **not** compute efficiency as `directCost / totalCost` or any other cost-vs-cost ratio — that is a different quantity and does not correspond to anything the book defines. The numerator is Σ effort (activity durations, summed, assuming perfect utilization); the denominator is the actual project cost (staffing integrated over the duration — the area under the staffing distribution chart).

**High efficiency is bad, not good.** A well-designed, correctly staffed project runs 15–25%. Higher — 40% is called out as "simply impossible to build" — means the network is mostly critical or near-critical: little or no float, so any slip on any activity cascades straight into the deadline. This is why App C §4.6f caps it at **25%**: the cap is a brittleness ceiling, not a "be more efficient" target. If an option's efficiency reads high, the fix is *more* float or *less* elastic staffing assumption, not a change of formula.

Worked example to reuse when this skill needs one (book's own numbers, fully specified and checkable): Σ effort = 10 man-months, project cost = 50 man-months ⇒ efficiency = 10/50 = **20%**.

> **Recorded defect (2026-08-10).** An earlier revision of this skill's worked example (the Step 6 comparison table) showed direct/indirect/total cost of 24/12/36 man-months against normal, 32/9/41 against compressed, and labeled the efficiency row 17% → 22%. Neither number is `directCost / totalCost` (24/36 = 67%, not 17%) or any other cost-ratio of the figures shown — no formula reproduces them. This sent a live compression search to a confidently wrong conclusion ("the project is already over the efficiency ceiling, compression is forbidden") before it was caught against the committed risk model. The example below is corrected: efficiency is computed from a stated Σ effort, per the formula above, never from the cost rows alone.

## Plan vs. option: derived edges are immutable in one, mutable in the other

[[the-method-activity-list]]'s rule — *no exclusions, no derived-edge overrides* — governs the **plan** (`.activityList` / `.network`, slots 9–10): if a derived dependency edge is wrong there, the architecture relationship it comes from is wrong, and the fix is in `.systemDesign`, not in the plan.

That rule does **not** carry over to this option. An **option** (`.compressedSolution` and its siblings) is not an assertion of what is true of the project — it is an assertion of what *would* be true under a staffing/sequencing choice. Compression's entire technique is to deliberately break a real dependency edge and repay it later at integration (a simulator, a design-first split). Conflating the two — treating an option's edges as if they carried the plan's immutability, or worse, changing the schedule with a scalar that asserts nothing about *how* the edges moved — is what made the retired `criticalSpeedup` fudge factor possible in the first place. It changed duration without changing the network; nothing about it was checkable against the architecture, and a compressed option built from it must be treated as unverified, not as compressed.

So: this option's network state carries its own edges, separate from `.network`. Every technique in this skill mutates *that* copy. `.network` itself never changes.

## Input

State is git-as-DB: all of this lives in `.aiarch/state/project.json` (a typed JSON aggregate), NOT in `designs/<product>/*.md` or `network.yaml` files. Markdown/DSL/YAML is a render-on-read of the typed state, never the source of truth.

- The committed **normalSolution** artifact in `project.json` → `.normalSolution` (the baseline being compressed, with its resource-assigned network state) and the committed **network** artifact → `.network`
- The committed **planningAssumptions** artifact in `project.json` → `.planningAssumptions`

## Output

The compressed solution is a **typed model committed into `.aiarch/state/project.json` → `.compressedSolution`** — git is the database. It carries this option's own recomputed network state (compression extras and resource swaps), summary metrics, and the comparison-to-normal table. It is NOT a `compressed.md` file; any markdown (including the Step 6 template) is a render-on-read of that JSON slot.

Two usage patterns produce this slot:

1. **Agentic/CI dispatch:** the agent produces the typed `CompressedSolution` model as JSON and commits it into `.compressedSolution` on its session branch; the server reads it back and stages it (`StageArtifactForReview`) for the human review gate (`CommitArtifact` / `RejectArtifact`).
2. **Local interactive:** same — produce the typed model and write it into the `.compressedSolution` slot. Never a `designs/*.md` file.

## Procedure

### Step 1 — Apply quick-and-clean first (App C §4.6a)

Per App C: *"Accelerate the project first by quick and clean practices rather than compression."*

Before reaching for compression techniques, check:

| Quick-and-clean lever | Action |
|---|---|
| Are there hidden dependencies you can remove? | Re-examine each dependency in the committed `.network` slot |
| Can the team skip activities that don't add value? | Drop them (rare; usually each was added intentionally) |
| Can specialists replace generalists? | Hire / engage an expert per ch. 9 §1 |
| Is there parallel work the team simply hadn't planned? | Surface and add |
| Are activity estimates inflated by safety padding? | Right-size (don't crash; right-size) |

If quick-and-clean reduces duration meaningfully, use that as your compressed baseline before applying real compression.

### Step 2 — The closed move catalogue

Compression is expressed as a typed `moves[]` list on this option, applied to a *copy* of the plan's network — never as a scalar. There are exactly four kinds; a fifth is not a thing until this skill is amended to add it.

| Move | Effect on the option's network | Book grounding |
|---|---|---|
| `simulator {target, dependents, effortDays, riskBucket, justification}` | Insert `S-<target>`; redirect each named dependent's edge from `C-<target>` to `S-<target>`; **emit the integration-debt edge that repays the real dependency downstream.** | Ch. 9 §1 — simulators as a classic acceleration technique |
| `designFirst {target, designEffortDays, justification}` | Split `C-<target>` into `D-<target>` (contract design) + `C-<target>` (build). Dependents' edges move to `D-<target>`; `C-<target>` now depends on `D-<target>`. | Ch. 11 Table 11-5 / Ch. 13 "Adding Enabling Activities" — client/contract designed first |
| `topResources {speedup, targets, justification}` | Swap in higher-skilled resources on the named critical-path activities; each activity's duration divides by `speedup`, capped at **1.5**. | Ch. 9 §1, App C §4.6d — "compress with top resources carefully and judiciously" |
| `split {target, parts, justification}` | Divide one activity into `parts` parallel pipelines over the same effort (effort is conserved — this is parallelization, not a discount). | Ch. 9 §6 / Ch. 11 §4 — network compression by splitting a chain |

`simulator` and `designFirst` are the two moves that mutate the *dependency graph itself* — they are Löwy's "compress with parallel work rather than top resources" (App C §4.6c), applied selectively. Apply them first. `topResources` is `criticalSpeedup`'s legitimate typed successor: it is retained as a move's *parameter*, not deleted, but it is now one bounded lever among four, argued per-activity with a justification, never a whole-option multiplier. Apply it second, per App C §4.6d, and only to activities actually on the critical path.

**Every move must repay its integration debt.** A `simulator` *defers* a dependency; it never deletes one — the edge from the simulator's stand-in back to the real dependency must still exist somewhere downstream, or the option is claiming free acceleration that was never paid for. A `designFirst` split that leaves the build activity (`C-<target>`) with no successor drops it off the schedule entirely — an activity with no incident edge is invisible to the network solve no matter how much effort it carries. Both were found live in this project: a `designFirst` move that split the design phase out but forgot to re-wire the build phase's dependents.

Löwy's "Client designs first" example (ch. 11 Table 11-5) is the canonical `designFirst`: pull a component's detailed-design phase into its own activity so others build against its *frozen contract* before it is fully built. Apply it selectively — only to components on the critical path that others build against (Managers, Engines, Clients) — never universally; the base activity list keeps design as an internal phase of the one per-component activity (per [[the-method-activity-list]]).

For each move: add it to this option's `moves[]`, recompute the option's network from the mutated graph, and re-run the four coherence invariants (below) before trusting the result.

### Step 3 — Iterate compression

Per ch. 11, compression is iterative. Each iteration:

1. Identify the current critical path.
2. Apply one compression technique to one or more critical activities.
3. Recompute the network.
4. The critical path likely shifts (a previously near-critical chain becomes critical).
5. Repeat on the new critical path.

Stop when:
- You hit the **compression target** (typical: 20–30% reduction from normal).
- You hit the **30% cap** (App C §4.6e: *"avoid compression higher than 30%"*).
- You hit the **death zone** (App C §4.6b: *"never commit to a project in the death zone"*).
- You hit the **efficiency ceiling** (App C §4.6f: efficiency > 25% means brittle).

### Step 4 — Identify the death zone (Ch. 9 §4 — critical)

The death zone is the region of the time-cost curve where any further compression is infeasible at any cost. Beyond it, no amount of money or people can deliver.

Per ch. 9: characterize the death zone via the time-cost curve (full curve built in [[the-method-risk-modeling]]). For now:

- Plot points: each compression iteration gives a (duration, total_cost) point.
- As you compress further, cost rises but duration reduction plateaus.
- The death zone is the asymptote — duration cannot go below a hard minimum no matter what cost you accept.

If your current compressed solution lies in the death zone region, **back off**. Per App C §4.6b: this is a non-negotiable rule.

## The four coherence invariants — solve and assert, never merely compare

Mutating edges (Step 2) is more aggressive than the plan's derivation ever was, and a network can satisfy every ordinary consistency check while still describing an impossible schedule. This is not hypothetical: Stage 1 of this project shipped a derived network claiming the system was fully tested 50 days before the managers it tests existed — every gate was green, the network was internally consistent, and it was still incoherent as a schedule. Compression's edge surgery reopens the same failure mode on purpose, so before this option's network is trusted for a comparison table, **solve it and assert** these four invariants over the *solved* result:

1. **Terminal-gate ordering.** The terminal `N-IT` system-testing gate finishes after every activity it gates. No compression move may leave a gated activity finishing later than the gate itself.
2. **Node completeness.** Solved node count equals activities + milestones in this option's plan. The node universe is built from *edges* — an activity with no incident edge in either direction vanishes from the solve, carrying its effort with it, however large. This is exactly how a `designFirst` split can silently drop the build activity off the schedule if its edges aren't re-wired.
3. **Integration debt survives.** Every `simulator` move still has, somewhere downstream, an edge repaying the real dependency it deferred. A simulator that removes its own integration debt is compression by accounting fraud, not compression.
4. **Deferral, not inversion.** A move may *defer* a dependency the architecture asserts; it may never *reverse* one. If the System says `A` depends on `B`, no option may finish `A` before `B` without a simulator explicitly standing in for `B` — silently finishing early with no stand-in is not compression, it's an incorrect schedule that happens to look faster.

These are assertions over the *solved* network (post-CPM), not comparisons between two unsolved shapes — a derive-and-compare pass cannot catch any of the four, by construction.

### Step 5 — Compute final compressed metrics

After iteration converges (and invariants 1–4 hold):

| Metric | Compressed value |
|---|---|
| Duration | N days |
| Σ effort (all activities, perfect utilization) | N man-months |
| Direct cost | N man-months (higher than normal — parallel work and/or top resources added) |
| Indirect cost | N man-months (lower than normal — duration is shorter) |
| Total cost | direct + indirect (usually higher than normal, despite indirect drop) |
| Efficiency | Σ effort ÷ total cost, N% (verify ≤ 25% — see "Project efficiency" above; NEVER `directCost / totalCost`) |
| Peak staffing | N (higher than normal due to parallel work) |
| Compression vs normal | N% (verify ≤ 30%) |

App C reminder: compress the project *even if the likelihood of pursuing any of the compressed options is low* (§4.6g). The exercise reveals the time-cost shape, which informs every other option.

### Step 6 — Commit the typed compressed solution to `.compressedSolution`

Produce the typed `CompressedSolution` model and commit it to `.aiarch/state/project.json` → `.compressedSolution`. The markdown below is the equivalent **human rendering** — use it to review the solution, but the source of truth is the slot, not a `compressed.md` file:

```markdown
# Compressed Solution — <Product>

## Summary (vs. normal)

| Metric | Normal | Compressed | Delta |
|---|---|---|---|
| Duration | 120 days | 90 days | -25% ✓ within cap |
| Σ effort (all activities) | 7 mm | 8.5 mm | +21% (added simulator + design-first enabling activities) |
| Direct cost | 24 mm | 32 mm | +33% (parallel work + top resources) |
| Indirect cost | 12 mm | 9 mm | -25% (shorter duration) |
| **Total cost** | **36 mm** | **41 mm** | **+14%** |
| Efficiency (Σ effort ÷ total cost) | 7/36 = 19% | 8.5/41 = 21% | within 25% cap ✓ |
| Peak staffing | 5 | 8 | parallel work needed |
| Preliminary risk | 0.45 | 0.62 | higher |
| Compression from normal | — | 25% | within 30% cap ✓ |
| Death zone | — | No | ✓ |

Efficiency here is Σ effort divided by total cost — never `directCost / totalCost` (that would read 32/41 = 78%, a different and irrelevant quantity). See "Project efficiency" above for the book's own 10/50 = 20% worked example.

## Compression techniques applied

### 1. Break dependency edges — `simulator` and `designFirst`
- `simulator {target: C-PricingEngine, dependents: [C-CheckoutManager], ...}` — built `S-PricingEngine` so `CheckoutManager` construction didn't wait; integration debt repaid at `I-PricingEngine-real` before system testing
- `designFirst {target: C-OrderManager, designEffortDays: 10, ...}` — split into `D-OrderManager` (contract) + `C-OrderManager` (build); dependents re-wired to `D-OrderManager`

### 2. Parallelize — `split`
- `split {target: C-OrderManager, parts: 2, ...}` — workflow and state-handling pipelines in parallel over the same conserved effort, added junior-dev-4

### 3. Top resources — `topResources` (secondary, capped at 1.5)
- `topResources {speedup: 1.5, targets: [C-OrderManager], ...}` — senior-dev-1 (top-tier) on the Manager build instead of junior — duration 15 → 10 days

## New critical path
A001 → A002p (split) → A012 → ... → AN
Total: 90 days

## Resource assignments
| Activity | Resource | Days | Float |
|---|---|---|---|
...

## Staffing distribution
<chart — shows higher peak than normal>

## Planned earned value
<chart — steeper but still shallow-S>

## Costs (man-months)
| Cost | Compressed |
|---|---|
| Direct | 32 |
| Indirect | 9 |
| Total | 41 |

## Risk flags
- Parallel pipelines increase coordination overhead
- Bus-factor on top-tier senior-dev-1 (now on critical path)
- Soft UX-construction dependency: risk if UX runs late

## When to choose compressed over normal
- Hard deadline that normal would miss
- Revenue/strategic value of earlier delivery > N man-months in extra cost
- Team genuinely has the bench depth for parallel pipelines
```

## Draft-job doctrine (CI dispatch)

This is the normative task the CI draft job (and a local `/project-design` run) executes to produce the `CompressedSolution`. It is self-contained: everything a draft agent needs to design the compressed option is stated here.

Design the COMPRESSED solution: shorter duration via `simulator`/`designFirst` edge-breaking moves first, `split` second, `topResources` (capped at 1.5) third — never via a whole-option scalar. Compression beyond ~30% of the normal duration is the death zone — target a modest compression (well under 30%) and stop short of it. Every move's justification is grounded in a stated property, never a superlative (per [[the-method-activity-list]]'s "Ground a justification in a property" correction — the same discipline applies here).

### Solution class rates

classRates MUST be the PlanningAssumptions rateCard derivation — for each class: megatokensInPerDay×(input $/MTok) + megatokensOutPerDay×(output $/MTok) for its modelId, in USD minor units — IDENTICAL across all four solution options (the workers are AI agents; a class's day-cost does not change between options). Option economics differ ONLY through duration, staffing cap, calendar, and buffer — never through invented per-day rates.

## Exit criteria (for router)

`.aiarch/state/project.json` → `.compressedSolution` holds a committed typed model with:
- Comparison table showing duration shorter, total cost higher
- `moves[]` naming every `simulator`/`designFirst`/`topResources`/`split` applied, each with a written justification
- Compression ≤ 30%
- Efficiency ≤ 25%, computed as Σ effort ÷ total cost (never `directCost / totalCost`)
- Not in death zone
- The four coherence invariants hold over the *solved* option network (terminal-gate ordering, node completeness, integration debt survives, deferral-not-inversion)
- New critical path identified

Move to `the-method-risk-modeling`.

**A viable compression is not guaranteed to exist.** If every candidate move set either fails a coherence invariant, exceeds the efficiency ceiling, or lands in the death zone, the correct output is a compressed option that says so — excluded, with the reason — not a scalar that manufactures one. A plan asserting compression is available when the search found none is worse than no compressed option at all (see the recorded case below).

## Recorded case: this project's own numbers (2026-08-10)

Worth keeping as a worked reference for what "no viable compression" looks like when it's real, not asserted:

- Normal solution: efficiency **24.76%**, five days of duration short of the 25% ceiling.
- A real, coherence-checked compression of **2.38%** existed in the search space — but it pushed efficiency past 25%, so App C §4.6f blocked it. The search correctly reported "no viable compression," not "compression forbidden by mistake" — the two look identical from the outside until you check *why*.
- The compressed option that had actually been committed before the moves search existed sat at **27.13% efficiency** with an empty `moves[]` and `criticalSpeedup: 1.8` still present — a scalar producing a schedule with no recorded technique behind it, excluded by the risk model as brittle. This is the shape of the retired defect: a number that changes the schedule while asserting nothing about how.

The lesson generalizes: **compression is not always available, and a plan that says otherwise is asserting rather than deriving.** An excluded compressed option, with its reason stated, is a correct and useful output of this skill — not a failure to find one.

## Anti-patterns to reject

- **A whole-option scalar (`criticalSpeedup` or equivalent) standing in for `moves[]`.** This is the retired defect: it changes the schedule while asserting nothing about how. Every duration change must trace to a named move with a justification.
- **`directCost / totalCost` (or any other cost-vs-cost ratio) reported as efficiency.** It is not the same quantity as Löwy's Σ effort ÷ total cost and does not correspond to anything the book defines — see "Project efficiency" above and the recorded defect it documents.
- **A `simulator` with no downstream integration-debt edge.** Compression by accounting fraud — the deferred dependency was never repaid.
- **A `designFirst` split that leaves the build activity with no successor.** It vanishes from the solved network carrying its effort with it; this was found live in this project.
- **Compression > 30%** — App C hard rule. Back off.
- **Death zone** — App C hard rule. Back off.
- **Top resources everywhere** — pays premium for activities with float. Apply only to critical path, capped at 1.5×.
- **Parallel work without resource backing** — pretending two juniors can work on one component simultaneously (App C §4.2g: 1:1 component-to-developer).
- **Indirect cost ignored** — compression saves indirect but costs direct. Show both.
- **Skipping the death-zone check** — non-negotiable per App C.
- **Skipping the coherence invariants** — a network can pass every ordinary consistency check and still be an impossible schedule (see "The four coherence invariants" above). Solve and assert; never merely compare.
- **Asserting a compressed option exists when the search found none.** See "Recorded case" above.
