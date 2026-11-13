# /acceptance-test-plan <activityId>

> The Acceptance Test Plan step (N-ATP): bind the use-case paths no single client plan covers, so the M-ACCEPT milestone can run them against the composed app and a failure routes to a named activity.

**Arguments** — `$ARGUMENTS` is `<activity_id>` (one token). Work lands on the shared activity branch `activity/<activity_id>` and its single PR — you are contributing commits to a PR that already exists. Do NOT open a second PR.

**Agent + skills.** Work to the standard of the **`test-engineer`** agent (`.claude/agents/test-engineer.md`). Follow **[[the-method-testing]]** (§ Deterministic component scenarios) and **[[the-method-project-state]]** for all reading/updating of `.aiarch/state/project.json`.

**Goal / intention.** Each client's own test plan proves that client against its own contract. Some use-case paths no single client projection can cover: the path an agent walks through the REST API and a member sees in the browser, the path an event enters before any client, the outcome that comes back to an actor who did not make the call. Those paths are the *residual*, and the platform computes it from the committed use cases (`listAcceptanceScenarios`); you never choose it. Your job is the *binding* of that residual into one acceptance plan, in the same declarative record as every other plan. Done means every residual variant is bound and the ATP-* and TP-* rules pass. This step must NOT write test code, must NOT add or drop variants, and must NOT bind against the implementation (you see the contracts and the use cases, never the source).

**Checks.** Call `run_checks()` until it reports every check passing, and finish with one full `run_checks()` call. Do not run the checks yourself.

## Steps

> **Operator notes first.** First call `getOperatorNotes`; if it returns notes, act on them before anything else. They are the operator's steer for this attempt (a send-back, retry or re-queue note) and outrank the defaults below.

1. Call `listAcceptanceScenarios`. It returns the residual: each acceptance variant (`<scenario>@<client>`, the use-case path as one actor walks it through that client) with its stimuli and why it is residual. Bind only those variants `[ATP-BOUND]`: a variant that is not in the residual is covered by a client plan and is refused, and so is a residual variant left unbound or bound twice.
2. **The binding is a record, not code.** For every residual variant write one binding `{scenario, arrange, steps, skip}` keyed by the variant id, and for every stimulus one step `{seq, operation, inputs, expect, probes}`, exactly as a client plan does (the TP-* rules hold every step against the contract of the component its stimulus addresses; a step's operation is that component's, so a path that leaves one client mid-way has steps on two contracts).
   - **Shared step inputs are named, not restated.** An input the client plan already binds is `{"name":"<param>","value":{"kind":"planRef","model":{"component":"<client plan key>","scenario":"<scenario id>","seq":<n>,"input":"<param>"}}}`: it resolves to the literal that plan binds `[ATP-REF]`. The reference must resolve to a literal (never to a `ref`), must name a parameter of the same schema as the input that carries it, and is refused in any client plan. An input no client plan binds is a literal or a `ref` as everywhere else.
   - **A cross-actor observation is a probe run as that actor.** When the use case shows an actor something after a stimulus another actor made, add a read-only probe for that node with `"as": "<actor id>"` `[ATP-PROBE-AS]`: the actor must be one of the plan's principals. Without `as` a probe runs as the step's own actor. A browser surface's probe is a read-only web operation (an `OpenList`-style read with one `navigate` action and the observables).
   - **`principals`**, on the plan, name every actor that originates a stimulus of a variant you bind, as in a client plan `[TP-PRINCIPAL]`; an agent actor's principal declares `onBehalfOf` `[TP-PRINCIPAL-DELEGATION]`.
   - **`browserActor`**, on the plan, names the actor whose browser every web step and web probe runs in, one of the principals `[ATP-BROWSER]`. The app serves one browser per launch, so a web step or probe run as any other actor is refused. A plan with no web step or probe needs none.
   - Every step asserts something, every expected state and output has its probe, a value the component mints at run time is a `ref` `[TP-EXPECT-REQUIRED]` `[TP-STATE]` `[TP-ARG-REF]`; never leave a blank placeholder.
3. A variant may be skipped only with `skip: {reason, until: <client activity id>}` naming the activity of the client whose design is not yet committed (for a web surface, its UI design). The skip ends when that activity is integrated; the milestone re-runs these rules and a stale skip holds it `[TP-SKIP]`.
4. `recordPhaseArtifact` with mapKey `acceptance` and payload `{"acceptancePlan": {...}}`. There is one acceptance plan per project, always filed under `acceptance`. The verb fails on any ATP-* or TP-* finding, and each finding names the rule; fix and retry.
5. `publishDraft`.
6. **Stop.** Do not mark phase status (the Manager owns that) and do not merge. Leave the PR open for the gate.
