---
name: the-method-contract-design
description: Reference for service-contract design in Juval Löwy's The Method — contracts as consumer-facing facets of a service, the operations-per-contract and contracts-per-service metrics (area of minimum cost), logical consistency, and factoring. Use when designing, reviewing, or critiquing a component's contract(s) — how many, how big, and how to factor them.
---

# The Method — Contract Design

Pure reference skill. Holds the canonical rules for designing the public contract a service (component) exposes. Other skills (system-design-standard-check, validate-architecture, the construction/service-design activities) cite this instead of restating the rules.

Sources:
- Appendix B "Service Contract Design" — "Services and Contracts", "Contracts as Facets", "Area of Minimum Cost", "From Service Design to Contract Design", "Contract Design Metrics".
- Ch. 3 §3 (ResourceAccess) — "atomic business verbs, not CRUD/IO"; a Manager "can support more than one family of use cases … facets of the service".

## Contract vs. component (the core distinction)

A **component/service encapsulates a volatility** (the Method's decomposition rule). A **contract is the public interface a service exposes to its clients** — merely a set of operations they can call. These are NOT one-to-one:

- **A service can support more than one contract.** Each contract is a *facet* of the service to a particular consumer — like a person who is simultaneously an employee, a landlord, and a spouse. Different consumers see different facets of the same underlying entity.
- The "one contract per service" assumption is a simplifying default for reasoning about cost, not a rule. In reality a single service may support multiple contracts, and multiple services may support one contract.

So when you find several consumer-shaped interfaces over one encapsulated volatility (one aggregate, one consistency boundary), that is **normal contract factoring, not a decomposition error** — do not split the component to chase it, and do not merge the facets into one mega-interface.

## The metrics — keep every contract in the area of minimum cost

A contract too small forces clients to orchestrate (integration cost); too large is a grab-bag that couples unrelated concerns (implementation cost). Size each contract to the flat middle:

| Operations in a contract | Verdict |
|---|---|
| **3 to 5** | The target range — right in the area of minimum cost. |
| **6 to 9** | Still relatively fine, but drifting out of the sweet spot — look to factor. |
| **1** | Too thin — a facet you can express in a single operation is "pretty dull"; it usually belongs folded into a related facet. |
| **≥ 20** | **Reject unconditionally.** There are no circumstances where a 20-operation contract is benign. |

**Contracts per service: one or two.** If a service needs **three or more *independent* business aspects** expressed as contracts, that suggests the service is too big — reconsider the decomposition. The trigger is *independent* aspects: facets of one indivisible aggregate (shared consistency/version boundary, no legal way to separate them under the layering rules) are not independent, and a residual >2 there is a justified exception, not a smell.

## Logical consistency beats the count

The metrics are necessary, not sufficient. **A contract must be logically consistent** — every operation a genuine facet of the same concern:

- A 4-operation contract (dead center of the 3–5 range) is still **rejected** if its operations are unrelated. Löwy's example: a device contract mixing `ReadCode()` (the reader-as-code-provider facet) with `OpenPort()`/`ClosePort()` (the reader-as-communication-device facet) is a grab-bag — reject it regardless of size.
- **Each contract/facet must stand alone**, operating independently of the service's other contracts. Do not condition one facet's behavior on another (the employment-status-conditioned-on-address anti-example).
- **ResourceAccess contracts expose atomic business verbs, not CRUD/IO.** `Select/Insert/Delete` in an access contract leaks the underlying resource; name the business operation and convert to CRUD internally.

## Factoring

When a contract is inconsistent or oversized, **factor** rather than tolerate:

- **Factor sideways** — pull the unrelated operations into a separate standalone facet-contract (`ICommunicationDevice` out of `IReaderAccess`). Both are contracts of the same service.
- **Factor up** — when facets share a weakly-related common core, hoist it into a base contract and let the specific facets extend it (`IReaderAccess : IDeviceControl`). Use only when the logical relation is genuinely weak; do not manufacture hierarchy.
- Consumer ergonomics for a large legitimate contract come from **consumer-declared (accepted) interfaces** in the calling component — a narrow view of the producer's contract — not from splitting the producer into more contracts or more components.

## Review checklist

- [ ] Each contract is a single logically-consistent facet — no grab-bag of unrelated operations.
- [ ] Operations per contract in 3–5 (6–9 tolerated with a factoring note); **never ≥ 20**; not a lone 1-op facet.
- [ ] One or two contracts per service; 3+ only when the facets are provably non-independent (one aggregate / one consistency boundary that cannot be split under the layering rules) — and that exception is documented.
- [ ] Each contract stands alone; no cross-facet conditioning.
- [ ] ResourceAccess contracts name atomic business verbs, not CRUD/IO.
