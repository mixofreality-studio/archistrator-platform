# /service-test-plan <componentId> <activityId>

> The Test Plan step of a service activity: bind this component's deterministic scenarios to concrete values, so the tests exist before construction fills their hooks.

**Arguments** — `$ARGUMENTS` is `<component_id> <activity_id>` (two space-separated tokens). Parse once; do not swap them. Work lands on the shared activity branch `activity/<activity_id>` and its single PR — you are contributing commits to a PR that already exists. Do NOT open a second PR.

**Agent + skills.** Work to the standard of the **`test-engineer`** agent (`.claude/agents/test-engineer.md`). Follow **[[the-method-testing]]** (§ Deterministic component scenarios) and **[[the-method-project-state]]** for all reading/updating of `.aiarch/state/project.json`.

**Goal / intention.** Per ch. 9, a test engineer is "not a tester, but a full-fledged software engineer" whose code exists to break the system; per App A the Service Test Plan precedes construction. On this platform the plan's *scenarios* are not yours to invent: they are derived deterministically from the committed use-case activity diagrams (the I/O-explicit activity-diagram technique — one stimulus per path, every elementary path, each loop unrolled once), and this component's plan is every scenario in which some stimulus is an input to it. Your job is the *binding*: the concrete values, expected results and probes that turn each derived scenario into a black-box integration test the venue can run against the real downstream stack. "Done" means every projected scenario is bound and the TP-* rules pass. This step must NOT write test code, must NOT add or drop scenarios, and must NOT bind against the implementation (you see the contract and the use cases, never the component source).

## Steps

> **Operator notes first.** First call `getOperatorNotes`; if it returns notes, act on them before anything else. They are the operator's steer for this attempt (a send-back, retry or re-queue note) and outrank the defaults below.

1. Call `listComponentScenarios` with the component id. Each scenario has stimuli (inputs into this component), expected outputs, expected states and hints. Do NOT invent scenarios; the set is derived from the use cases.
2. For every scenario, write one binding: per stimulus, `operation`, concrete `inputs` (values that satisfy the param schemas), `expect` (result subset or error), `probes` proving each expected state (`for` = node id) and any observable output, `unobservable` for outputs no contract can observe, and `hook: true` when arranging the guards needs non-declarative setup.
3. A scenario may be skipped only with `skip: {reason, until: <activityId>}` naming an upstream activity not yet integrated.
4. `recordPhaseArtifact` with mapKey = component id and payload `{"testPlan": {...}}`. It fails on any TP-* finding; fix and retry.
5. `publishDraft`.
6. **Stop.** Do not mark phase status (the Manager owns that) and do not merge. Leave the PR open for the gate.
