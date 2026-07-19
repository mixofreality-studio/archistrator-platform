---
name: the-method-code-health
description: Reference for construction-time code-health judgment — the enforcement ladder, comment philosophy, immutability-first modeling, ADT/generics discipline, design-first structure, DRY-without-premature-abstraction, layering discipline, and generator dogfooding. Use during construction and code review, once codegen and lint gates have run, to catch what those mechanical gates cannot.
---

# The Method — Code Health

This is a judgment skill, not a procedure. Codegen drift checks, `methodcheck`, and `golangci-lint`
mechanically enforce what can be reduced to a rule; this skill holds the residue — the calls a
construction agent or reviewer has to make when the mechanical gates fall silent. Most of it is
**house Go construction practice**, developed by running large code-health passes over Method-shaped
systems; only where a rule below actually derives from Juval Löwy's *The Method* is it cited as such.
Do not read a missing citation as an oversight — it means the rule is practice, not doctrine.

Read this skill during construction (writing code against a committed service contract) and during
code review, after the mechanical gates have already run clean. If you find yourself applying the
same piece of judgment from this skill more than a couple of times across a codebase, that is a
signal in its own right — see §1.

## 1. The enforcement ladder (meta-principle)

Every rule about code quality should live at the cheapest tier that can actually catch it:

| Tier | Enforcement | Example |
|---|---|---|
| (a) Codegen | The rule is structurally impossible to violate because generated code is the only way to satisfy the contract | A generated `.gen.go` client can't drift from its OpenAPI/contract source because it *is* derived from it |
| (b) Lint / methodcheck | A static check fails the build | `golangci-lint` exhaustive/sumtype checks; `methodcheck` rules that ban ResourceAccess-to-ResourceAccess calls or duplicated contract method sets |
| (c) Skill (this tier) | Judgment a human or agent applies at write-time or review-time, because it resists mechanical encoding | "Is this comment explaining WHY or restating WHAT?" |

**Prefer (a) over (b) over (c).** Tier (c) is not a resting place — it is a holding pen. When the same
judgment call from this skill gets applied repeatedly across a codebase, that repetition is itself
the signal to push the rule down the ladder: write the lint rule, or change the generator so the
violation can't occur.

**Corollary — never hand-edit generated files.** Anything under a `*.gen.go` (or equivalent generated)
path is owned by its generator, never by a human edit. Every generator needs a **drift gate**: run the
generator, `git diff --exit-code` against the committed output. A clean diff proves the committed
generated code is byte-identical to what the generator produces today — the only way tier (a)
enforcement stays true over time.

## 2. Comment philosophy

**Comments explain WHY. They never restate WHAT the code already says.** A comment that repeats the
next line in English is pure noise — worse than no comment, because it has to be maintained and
inevitably drifts from the code it's echoing.

| Scrub (delete on sight) | Keep |
|---|---|
| File-flattening seam markers (`// ---- from x.go ----`) left over from a merge/flatten pass | Genuine domain or Method rationale a future reader needs and can't derive from the code alone |
| Doc comments orphaned from a type that moved to a generated file — the comment no longer describes anything nearby | A note on *why* a non-obvious choice was made (e.g., why this ResourceAccess method retries on one error class and not another) |
| History or PR narration in the code itself — "founder ruling", "Replaces the former X", "Task N", disclosure tags | A pointer to an ADR, a book section, or a design artifact that motivates a structural choice |
| Restating the signature or the next line ("// increments count" above `count++`) | |

**Smell metric:** a file where roughly a third or more of its lines are comment-only lines is
over-commented — most of that mass is WHAT-noise, not WHY-signal. This ratio is cheap to compute and
worth guarding with a CI check once it has been used as judgment more than a few times (see §1's
ladder).

## 3. Immutability-first

Model state as **immutable value types**. A value that changes is a new value, not a mutation of the
old one — this is what makes state safe to share across goroutines, log, diff, and replay without
defensive copying.

Prefer **head-state aggregates** over full event-sourced replay where the actual volatility is
*current state*, not *history*. If nothing downstream needs the sequence of how a value arrived at
its current shape — only the shape itself — modeling it as replayable history is unearned complexity.
Reach for full event sourcing only where the history itself is a first-class need (audit, temporal
queries, compensating logic).

Pass **idempotency keys in**, rather than threading mutable state through layers to derive one.
Per the Temporal mapping in
[the-method-layers](../the-method-layers/SKILL.md#temporal-mapping-when-managers-run-on-temporal),
the Manager derives the key (e.g. `${workflowId}:${activityId}`) and passes it into the
ResourceAccess call, because the ResourceAccess method is plain and Temporal-free and cannot read
workflow context itself.

This section is house practice — Löwy's *The Method* is silent on immutability and event-sourcing as
implementation techniques; it governs component decomposition, not internal state representation.

## 4. ADT & generics judgment

**Model closed sets as sum types with exhaustive switches.** No silent `default:` fallback on a
closed set — a new variant must fail to compile (or fail lint) until every switch handles it. A
`golangci-lint` exhaustive/sumtype baseline enforces the mechanics, but the judgment this skill adds
is upstream of the lint: *reach for the sum type in the first place* when a value is genuinely a
closed, enumerable set of cases. Modeling a closed set as a string constant plus scattered
`if`/`else if` chains is the failure mode the lint can only catch after the fact.

**Introduce generics only when they remove real, present duplication** — e.g., a shared enum codec
used by a dozen types today. Do not generify speculatively for a duplication that might appear later;
a second concrete instance you actually have beats a generic abstraction over instances you imagine.

This is Go-specific construction judgment, not a Method principle.

## 5. Design-first & structure

**Design the contract and its volatility before writing code.** Per
[the-method-service-contract](../the-method-service-contract/SKILL.md), a component's service contract
is designed and reviewed before construction starts; a construction agent implements against a
committed contract, it does not discover the contract by writing code first.

**One file per component.** A component's implementation file is little more than the concrete
implementation of its generated contract. Do not de-flatten a component into many files "for
organization" — that scatters the one thing a reader needs (the contract's implementation) across
files they now have to reassemble. Do not go the other direction either and concatenate multiple
components into one file — that is functional decomposition's file-system cousin.

**One Go package per component, but a package may host multiple contract facets.** Per Löwy Appendix
B "Contracts as Facets" (cited in
[the-method-service-contract](../the-method-service-contract/SKILL.md#step-2--group-operations-into-contracts)),
a service may legitimately support more than one contract — up to two, per App B §5.4 / App C §6.4.
When it does, both facets live in the same package; do not force-split a single component's facets
into separate packages just because they are separate contracts.

**Accepted interfaces are the Go idiom for consumer-side dependencies.** A consumer that only needs a
subset of a contract declares its own narrow, usually-unexported interface naming just the methods it
calls, satisfied structurally by whatever concrete type provides them. This is Go-specific practice —
Go's structural typing makes it natural — not a Method rule. What *is* enforced (methodcheck rules
c/d, tier (b) on the ladder) is the corollary: never re-declare or duplicate a generated contract's
full method set in a hand-written interface. If you need the whole contract, import the generated
one; if you need part of it, write the narrow accepted interface.

## 6. DRY without premature abstraction

Extract a shared helper when duplication is **real and load-bearing** — a builder or codec invoked
dozens of times, where a change to the shape has to land in one place. Do not stand up a shared
package for two near-identical one-liners. In that case the "dedup" is whichever existing helper
already covers the common case, and a small number of identical one-liner call sites is an
**acceptable end state**, not a smell to keep chasing. Premature abstraction to satisfy a DRY
instinct on trivial duplication produces an indirection that costs more to read than the duplication
it removed.

## 7. Layering discipline

Cross-link [the-method-layers](../the-method-layers/SKILL.md) for the full interaction-rule table;
this section only calls out the constructions that recur as code-health findings.

- **Temporal is a Manager-layer concern only** (book-derived rule, house infrastructure mapping — see
  [the-method-layers](../the-method-layers/SKILL.md#temporal-mapping-when-managers-run-on-temporal)).
  ResourceAccess and Engines import no Temporal package and contain no Temporal type. A layer-error →
  durable-execution error bridge maps ResourceAccess/Engine faults into the Manager's Activity
  failure handling; the idempotency key is derived and passed in by the Manager (§3), never read from
  Temporal context by the lower layer.
- **ResourceAccess owns its data models.** A ResourceAccess component's data types live with the
  ResourceAccess, not in a separate "domain" package or layer. This is house practice: *The Method*
  defines the ResourceAccess layer's responsibility (atomic business verbs over a Resource, never
  CRUD) but does not mandate a package layout for its data types; keeping them co-located is a
  construction convention that avoids inventing a sixth layer the book doesn't have.
- **No ResourceAccess-to-ResourceAccess sideways calls.** This one *is* book-derived — App C §3.6, and
  restated in [the-method-layers](../the-method-layers/SKILL.md#interaction-donts): "ResourceAccess
  components never call each other (use a single ResourceAccess that joins multiple Resources
  instead)." Where two ResourceAccess components seem to need each other, either join them into one
  ResourceAccess over multiple Resources, or route the coordination up through a Manager.
- **Composition-root ports for wiring, not sideways imports.** Wire dependencies at the composition
  root using accepted interfaces (§5); a component that imports a sibling component directly (rather
  than depending on a narrow interface satisfied by it) is the sideways-call smell above, wearing a
  Go-import disguise.

## 8. Dogfooding — no app-specifics in generators

A reusable generator (one that ships in method-assets and runs across every app that materializes it)
must never hardcode a consuming app's component or contract names. If a generator needs to know
"which components exist" or "which contract a component implements", that fact belongs in **data** —
a field in the project model / `project.json` schema — not in a string literal or a switch case inside
the generator's Go source. A generator with an app-specific branch has stopped being a generator and
become a one-off script wearing a generator's file extension; the fix is always to push the
app-specific fact down into the data the generator already reads, not to special-case the generator.

## How to cite

When another skill needs a code-health judgment call during construction or review, link to this
file and the specific section — do not restate the ladder, the comment rules, or the layering
call-outs inline. For example:

> Per [the-method-code-health](../the-method-code-health/SKILL.md), a lint rule that catches the same
> review comment twice belongs on the enforcement ladder, not repeated in review a third time.

## See also

- [the-method-layers](../the-method-layers/SKILL.md) — the canonical layer model and interaction
  rules this skill's §7 draws its book-derived rules from.
- [the-method-service-contract](../the-method-service-contract/SKILL.md) — the contract-design
  procedure this skill's §5 assumes runs before construction starts.
- [the-method-doctrine](../the-method-doctrine/SKILL.md) — the Prime Directive and 9 directives;
  Directive 1 (avoid functional decomposition) is the book-level version of this skill's file/package
  and generator-dogfooding judgment in §5 and §8.
