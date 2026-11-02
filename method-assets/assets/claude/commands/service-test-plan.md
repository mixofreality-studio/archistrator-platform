# /service-test-plan <componentId> <activityId>

> The Test Plan step of a service activity: bind this component's deterministic scenarios to concrete values, in parallel with construction, so the integration task can generate the black-box tests from them and run them against the integrated component.

**Arguments** — `$ARGUMENTS` is `<component_id> <activity_id>` (two space-separated tokens). Parse once; do not swap them. Work lands on the shared activity branch `activity/<activity_id>` and its single PR — you are contributing commits to a PR that already exists. Do NOT open a second PR.

**Agent + skills.** Work to the standard of the **`test-engineer`** agent (`.claude/agents/test-engineer.md`). Follow **[[the-method-testing]]** (§ Deterministic component scenarios) and **[[the-method-project-state]]** for all reading/updating of `.aiarch/state/project.json`.

**Goal / intention.** Per ch. 9, a test engineer is "not a tester, but a full-fledged software engineer" whose code exists to break the system; per App A the Service Test Plan is its own branch of the activity, written beside construction and joined with it at testing. On this platform the plan's *scenarios* are not yours to invent: they are derived deterministically from the committed use-case activity diagrams (the I/O-explicit activity-diagram technique — one stimulus per path, every elementary path, each loop unrolled once), and this component's plan is every scenario in which some stimulus is an input to it. Your job is the *binding*: the concrete values, expected results and probes that turn each derived scenario into a black-box integration test the integration task runs against the real downstream stack. "Done" means every projected scenario is bound and the TP-* rules pass. This step must NOT write test code, must NOT add or drop scenarios, and must NOT bind against the implementation (you see the contract and the use cases, never the component source).

**Checks.** Call `run_checks()` until it reports every check passing, and finish with one full `run_checks()` call. Do not run the checks yourself.

**Sent back from integration.** If your operator notes report plan defects, integration has alleged that named scenarios' bindings contradict the contract or use case. The note is a claim to check, not evidence. Re-derive only the named bindings from the contract and the use case, and change them only where the claim holds; if a binding follows from the contract and use case, leave it exactly as it is, because integration must then fix the component. Do not add or drop scenarios, and do not change a binding the note does not name. Never copy a value from the implementation or from an observed result into a binding.

## Steps

> **Operator notes first.** First call `getOperatorNotes`; if it returns notes, act on them before anything else. They are the operator's steer for this attempt (a send-back, retry or re-queue note) and outrank the defaults below.

1. Call `listComponentScenarios` with the contract key (once per contract facet, when the component has several). Each scenario has stimuli (inputs into this component), expected outputs, expected states and hints. Do NOT invent scenarios; the set is derived from the use cases.
2. For every scenario, write one binding: per stimulus, `operation`, concrete `inputs` (values that satisfy the param schemas), `expect` (result subset or error), `probes` proving each expected state (`for` = node id) and any observable output, `unobservable` for outputs no contract can observe, and `hook: true` when arranging the guards needs non-declarative setup.
   - **There is no step-output reference.** A value is a literal or it is a hook. An input whose value the component mints at run time, such as an id returned by an earlier step, cannot be written as a literal: mark that step `hook: true`. Never write prose such as `id from step 2` as a value.
   - **Every step carries a real value and a real expectation.** Never leave a blank placeholder: an empty string, an empty object or a missing `expect` where the contract promises something is a defect, not a default.
   - **Bind what the error model says.** A value the contract's error model refuses must be bound as `errorExpected` with its code. An empty credential the contract refuses cannot expect success.
3. A scenario may be skipped only with `skip: {reason, until: <activityId>}` naming an upstream activity not yet integrated.
4. `recordPhaseArtifact` with mapKey = the contract key (the `.serviceContracts` key, e.g. `billingEngine` for `billing-engine`) and payload `{"testPlan": {...}}` whose `component` is that same key. A plan is filed under its contract key and nothing else: the generated test, its results and the checks that read them are all named by it. A component publishing several contract facets has one plan per facet, each under its facet's key. The verb fails on any TP-* finding; fix and retry.
5. `publishDraft`.
6. **Stop.** Do not mark phase status (the Manager owns that) and do not merge. Leave the PR open for the gate.
