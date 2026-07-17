# /core-use-cases-critique

> PM critique of the drafted **Core Use Cases** artifact — one design-rail CI job. Verdict only; the PM never rewrites the model.

**Arguments** — none. Kind, job mode, branch, and project come from the ambient `AIARCH_*` env.

**Agent + skills.** **Adopt the `product-manager` role**: read `.claude/agents/product-manager.md` and act per that charter yourself, in this session. Do NOT dispatch a subagent or use an Agent/Task tool — the CI job runs single-session; you ARE the product-manager. Judge against **[[the-method-core-use-cases]]** and [[the-method-project-state]] for the tool flow.

## Steps

1. **Read the draft** with `getDraftSlot` and its committed predecessors with `getCommittedSlot`.
2. **Read the ledger first** with `getReviewThread`. If you have critiqued before, critique the **delta** since your last verdict — not the artifact from scratch. Read `getCritique` to see your own prior verdict and notes — critique the delta against them.
3. **Apply verdict discipline** (anti-thrash — binding):
   - "revise" REQUIRES new, actionable comments tied to specific artifact content.
   - Never relitigate a resolved thread: if the architect responded to your comment, either accept the response or approve-with-noted-reservation. Repeating an already-answered comment is a defect.
   - Severity honesty: only defects against the mission/requirements justify "revise". Taste-level preferences are recorded as comments on an **approve**.
   - Co-discovery: you co-discover; if you object, say which raw use case is core and why — customer reality is your authority, abstraction taste is the architect's.
   - Core-use-cases doctrine you MUST enforce (ch. 4 §2.1: a core use case is an abstraction of the business essence, not a customer-listed feature):
     - **Count.** REVISE if there are fewer than 2 or more than 6 core use cases — more than 6 means the architect has not abstracted enough; exactly 1 may mean over-abstraction (look for distinct business pillars).
     - **Rejection trail.** REVISE if any nonCore use case lacks a rejection reason, or names a `variationOf` that does not resolve to a core use case's name.
     - **No channel-shaped use case.** REVISE if any use case is really a delivery channel or client surface (the mobile/web/API/voice version of the same behavior) — channels are permutations of one behavior, never separate use cases.
     - **No CRUD core.** REVISE if any core use case is create/read/update/delete mechanics ("Create Order", "Update Order") — mechanics are never business essence.
     - **Volatility exercise.** REVISE if the flows never cross the committed `.volatilities` seams: each accepted volatility should be exercised by at least one core flow, or its absence explicitly justified. Flows that exercise no committed volatility are restating the customer's current manual process, not the product's required behavior.
     - **Abstraction, not restatement.** REVISE if the core list is the customer's own task names unchanged — real subsumption means several raw use cases collapse under a higher (often newly named) abstraction; verify that the cores actually subsume raw entries rather than relabel them one-for-one.
     - **Actors and lanes.** REVISE if any use case has no actors, or an activity diagram's swim-lane `roleName`s do not resolve against that use case's declared actors.
4. **Record the verdict** with `setCritiqueVerdict` (approve/revise + comments). You MUST record your verdict with `setCritiqueVerdict` before finishing — a critique job that ends without recording a verdict fails the pipeline.
5. **Finish** with `publishDraft` (exactly once). You have no `putDraftModel`; do not attempt to fix the model yourself.
