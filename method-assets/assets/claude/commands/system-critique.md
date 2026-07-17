# /system-critique

> Architect critique of the drafted **System** (architecture) artifact — one design-rail CI job. Verdict only; the critic never rewrites the model.

**Arguments** — none. Kind, job mode, branch, and project come from the ambient `AIARCH_*` env.

**Agent + skills.** **Adopt the `system-architect` role**: read `.claude/agents/system-architect.md` and act per that charter yourself, in this session. Do NOT dispatch a subagent or use an Agent/Task tool — the CI job runs single-session; you ARE the system-architect, sitting as a fresh reviewer of the drafted decomposition. Judge against **[[the-method-architecture]]** (the Step 4 anti-pattern doctrine) and [[the-method-layers]], with [[the-method-project-state]] for the tool flow.

## Steps

1. **Read the draft** with `getDraftSlot` and its committed predecessors with `getCommittedSlot` — you need `.volatilities` and `.coreUseCases` in hand; every check below is against them, not against taste.
2. **Read the ledger first** with `getReviewThread`. If you have critiqued before, critique the **delta** since your last verdict — not the artifact from scratch. Read `getCritique` to see your own prior verdict and notes — critique the delta against them.
3. **Apply verdict discipline** (anti-thrash — binding):
   - "revise" REQUIRES new, actionable comments tied to specific artifact content (name the Component, Relationship, or DynamicView).
   - Never relitigate a resolved thread: if the drafter responded to your comment, either accept the response or approve-with-noted-reservation. Repeating an already-answered comment is a defect.
   - Severity honesty: only defects against the Method doctrine, the committed `.volatilities`, or the committed `.coreUseCases` justify "revise". Taste-level preferences (naming polish, diagram cosmetics) are recorded as comments on an **approve**.
   - Architecture doctrine you MUST enforce (ch. 3–5; Directives 1 and 2: avoid functional decomposition, decompose based on volatility):
     - **No functional or domain decomposition.** REVISE if Managers are named after use cases, phases, features, or domain nouns (`OrderProcessing`, `UserService`, a `<Phase>Manager` per workflow step) rather than the workflow volatility they hide — the name must answer "what change does this component contain?", not "what feature does it do?". REVISE for services explosion: |Managers| ≈ |core use cases| with one-for-one name mirroring is the use-case list restated as components, not a decomposition.
     - **Every committed volatility has a home.** REVISE if any volatility in the committed `.volatilities` is encapsulated by NO component, or by more than one. A deliberate deferral (e.g. to the `.operationalConcepts` artifact) must be stated as an explicit disposition on the draft — silence is a defect, not a disposition.
     - **Layer fit.** REVISE if a component's claimed volatility class does not match its layer: a workflow/sequence volatility belongs in a Manager, a business-rule/activity volatility in an Engine, a storage/IO/access-scheme volatility in a ResourceAccess. A misfiled volatility means the component is misclassified or the volatility is misstated.
     - **Almost-expendable Manager.** REVISE if a Manager fronts a single pass-through verb (one call in, one call down, no orchestration) — it is either a missing workflow volatility or a component that should not exist.
     - **Trigger honesty.** REVISE if a timer- or schedule-triggered use case's dynamic view originates at a synchronous client call — its chain must originate at a timer/schedule seam. A delivery or notification obligation with no structural seam anywhere in the model is the same defect: the architecture claims a behavior it cannot trigger.
     - **Dynamic-view completeness and legality.** REVISE if any use case in the committed `.coreUseCases` (core or non-core variation) lacks a `DynamicView`, or a view carries an empty/generic title that does not name its use case. REVISE if any chain breaks closed layering: upward calls, layer skips (Client → Engine/ResourceAccess/Resource), sideways calls other than queued Manager→Manager, or more than one Manager entered from the Client.
     - **Business verbs, not CRUD.** REVISE if a ResourceAccess exposes create/read/update/delete-shaped verbs — atomic business verbs only.
     - **Golden-ratio smell.** REVISE if Managers heavily outnumber Engines — business activity is buried inside fat Managers; extract the Engines or justify their absence.
4. **Record the verdict** with `setCritiqueVerdict` (approve/revise + comments). You MUST record your verdict with `setCritiqueVerdict` before finishing — a critique job that ends without recording a verdict fails the pipeline.
5. **Finish** with `publishDraft` (exactly once). Do not call `putDraftModel` in this job — you critique; the redraft weaves your notes in, you never rewrite the model yourself.
