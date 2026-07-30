# /volatilities-critique

> Architect critique of the drafted **Volatilities** artifact — one design-rail CI job. Verdict only; the critic never rewrites the model.

**Arguments** — none. Kind, job mode, branch, and project come from the ambient `AIARCH_*` env.

**Agent + skills.** **Adopt the `system-architect` role**: read `.claude/agents/system-architect.md` and act per that charter yourself, in this session. Do NOT dispatch a subagent or use an Agent/Task tool — the CI job runs single-session; you ARE the system-architect, sitting as a fresh reviewer of the drafted volatilities list. Judge against **[[the-method-volatility-identification]]** (the PROPOSE → FILTER → RECORD doctrine and the ch. 2 filters) and [[the-method-project-state]] for the tool flow.

## Steps

1. **Read the draft** with `getDraftSlot` and its committed predecessors with `getCommittedSlot` — you need `.mission`, `.glossary`, and `.scrubbedRequirements` in hand; every check below is against them, not against taste.
2. **Read the ledger first** with `getReviewThread`. If you have critiqued before, critique the **delta** since your last verdict — not the artifact from scratch. Read `getCritique` to see your own prior verdict and notes — critique the delta against them.
3. **Apply verdict discipline** (anti-thrash — binding):
   - "revise" REQUIRES new, actionable comments tied to specific artifact content (name the volatility entry or rejected candidate).
   - Never relitigate a resolved thread: if the drafter responded to your comment, either accept the response or approve-with-noted-reservation. Repeating an already-answered comment is a defect.
   - Severity honesty: only defects against the Method doctrine, the committed `.mission`, `.glossary`, or `.scrubbedRequirements` justify "revise". Taste-level preferences (naming polish, rationale wording) are recorded as comments on an **approve**.
   - Volatility doctrine you MUST enforce (ch. 2 §3; Directive 2: decompose based on volatility):
     - **Axis assignment and independence.** REVISE if an entry sits on the wrong axis — a change in THIS customer's business over time belongs on `sameCustomerOverTime`; a variation across customers/markets/regulations today belongs on `allCustomersAtOneTime`. REVISE if an entry naturally spans BOTH axes — spanning both axes is functional decomposition in disguise (ch. 2): the entry is a feature restated as a volatility; it must be split or restated as the underlying change.
     - **Volatile, not variable.** REVISE if an entry is merely variable — solved by a conditional or by data, with no ripple across the system if left unencapsulated (§3.1). Only open-ended change that would invalidate the architecture belongs on the list.
     - **No solutions masquerading.** REVISE if an entry names a mechanism, technology, feature, or glossary term rather than the underlying change ("Redis volatility", "Embedded tool volatility") — the scrub of [[the-method-requirements-analysis]] must not leak back in here; restate as the change being insulated against.
     - **No siren-song / reflex entries.** REVISE for a by-reflex "Logging" or "Reporting" block with no specific business volatility behind it (§3.6) — "you always had one" is not a volatility.
     - **No nature-of-business or speculative survivors.** REVISE if an entry every competitor does identically survived the filter (§3.7 — do not encapsulate changes to the nature of the business), or an entry with no grounding in this business's requirements (frivolous speculation on a future change, §3.7).
     - **Rejected-ledger quality.** REVISE if the `rejected` array is empty (a zero-rejection filter did not filter), if any rejection's class is not one of `variableNotVolatile`/`natureOfTheBusiness`/`speculative`/`foldedInto`, or if a rejection reason merely restates its class instead of saying why THIS candidate fails.
     - **Traces validity.** REVISE if any accepted entry traces to a Required-Behavior id (`B-NN`) that does not exist in the committed `.scrubbedRequirements`, or carries empty `traces` WITHOUT an explicit longevity grounding in its rationale (the named thing, its historical change rate, the lifespan horizon — ch. 2 Volatility and Longevity). An entry that is both untraced and ungrounded is probably speculative.
     - **Count sanity.** REVISE if there are fewer than ~6 accepted entries (missed axes — re-run the iterative factoring) or more than ~15 (cataloguing every variation — the volatile-vs-variable filter was not applied aggressively enough).
4. **Record the verdict** with `setCritiqueVerdict` (approve/revise + comments). You MUST record your verdict with `setCritiqueVerdict` before finishing — a critique job that ends without recording a verdict fails the pipeline.
5. **Finish** with `publishDraft` (exactly once). Do not call `putDraftModel` in this job — you critique; the redraft weaves your notes in, you never rewrite the model yourself.
