# /frontend-test-plan <componentId> <activityId>

> The Flows step of a frontend activity: bind this UI surface's deterministic scenarios to concrete user actions and observables, so the flow tests exist before construction fills their hooks.

**Arguments** — `$ARGUMENTS` is `<component_id> <activity_id>` (two space-separated tokens). Parse once; do not swap them. Work lands on the shared activity branch `activity/<activity_id>` and its single PR — you are contributing commits to a PR that already exists. Do NOT open a second PR.

**Agent + skills.** Work to the standard of the **`test-engineer`** agent (`.claude/agents/test-engineer.md`). Follow **[[the-method-testing]]** (§ Deterministic component scenarios) and **[[the-method-project-state]]** for all reading/updating of `.aiarch/state/project.json`.

**Goal / intention.** Per ch. 9, a test engineer is "not a tester, but a full-fledged software engineer" whose code exists to break the system; per ch. 11 a Client's test plan is scheduled early, exactly like a service's. On this platform the *scenarios* are not yours to invent: they are derived deterministically from the committed use-case activity diagrams, and this surface's plan is every scenario in which some stimulus is a user action on it. Your job is the *binding*: for each stimulus the concrete user actions on the approved design's screens (`{"kind":"click|fill|navigate","target":"<ui-design element id>","value":...}`) and the observable that proves the expected outcome (`{"visible":...}` / `{"text":...}` / `{"url":...}`), turned into a Playwright flow the venue runs against the integrated system. "Done" means every projected scenario is bound and the TP-* rules pass. This step must NOT write test code, must NOT add or drop scenarios, and must NOT judge the design concept's aesthetics (you see the approved design and the use cases, never the surface's source).

## Steps

> **Operator notes first.** First call `getOperatorNotes`; if it returns notes, act on them before anything else. They are the operator's steer for this attempt (a send-back, retry or re-queue note) and outrank the defaults below.

1. Call `listComponentScenarios` with the component id. Each scenario has stimuli (user actions on this surface), expected outputs, expected states and hints. Do NOT invent scenarios; the set is derived from the use cases.
2. For every scenario, write one binding: per stimulus, `operation`, concrete `inputs` (an `action` per user step, targeting element ids from the approved UI design in `.phaseArtifacts.uiDesign[<component_id>]`), `expect` (the observable), `probes` proving each expected state (`for` = node id) and any observable output, `unobservable` for outputs no surface can observe, and `hook: true` when arranging the guards needs non-declarative setup.
3. A scenario may be skipped only with `skip: {reason, until: <activityId>}` naming an upstream activity not yet integrated.
4. `recordPhaseArtifact` with mapKey = component id and payload `{"testPlan": {...}}`. It fails on any TP-* finding; fix and retry.
5. `publishDraft`.
6. **Stop.** Do not mark phase status (the Manager owns that) and do not merge. Leave the PR open for the gate.
