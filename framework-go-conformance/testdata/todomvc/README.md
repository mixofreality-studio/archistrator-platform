# todomvc conformance fixture

Provenance: the `project.json` harvested from paid bench run RUN-021
(`run-20261105T141816Z-04ce59ca`, 2026-11-05), produced by archistrator at
commit `b31bc72f` (server v0.8.139 / webapp v0.6.122). Component and contract
ids (`todo-web-app`, `list-manager`, ...) are the harvest's as-is.

Edits applied to the harvest by Plan 1 (the complete list of that step):

- `id` set to `todomvc-conformance`.
- Removed top-level `activityExecution`, `testingState` and `research`.
- `phaseArtifacts` reduced to `testPlan` and `uiDesign` (dropped `srs`,
  `integrationNote`, `uxRequirements`).
- `slots`, `serviceContracts` and `reviewPolicy` kept verbatim. The harvest has
  no top-level `moduleDir` key, so none is carried.

Fixtures live under `framework-go-conformance/testdata/` (not a `testdata/conformance/`
elsewhere) because `go:embed` cannot reference parent directories. The app-generator unit
tests read them by relative path (internal/testfixtures).

## Rebinding to binding model v2 (FPC plan 2, Task 2.13)

The harvest was admissible only under the pre-v2 rules. Plan 2's rules (UC-*,
CC-DECIDED-BY-ACTOR-STEP, SCN-*, TP-* v2, UID-*) refuse it with ~330 Error
findings. This is the one real app rebound by hand; `TestConformance_RUN021Proof`
holds the result to zero Error findings, with no waiver. Every judgement made:

### Use cases (slot 4) and dynamic views (slot 5)

- UC-START-NOT-ACTION: in all 12 use cases the labelled `start` in an actor's lane
  became an unlabelled start followed by the actor's act as its own `action` node
  `n1a` (lane and `linkedActorId` = the actor). The actor-to-Client calls of the
  system's first step moved onto a step of `n1a`; the Client-to-Manager calls stay.
- UC-PATH-OUTCOME / UC-OUTCOME-AT-ASKING-SURFACE: every path now ends in one
  `sendSignal` per originating actor (`out-<actor>`, in the actor's lane), placed after
  the last merge so refused and permitted paths both carry it. Where two actors originate
  (work-the-list, resume-the-list) there are two, in sequence.
- reword-a-todo (the RUN-021 Escape path): `n1a` is the member double-clicking the title
  (a call into `todo-web-app`); `n2` (decided by the member) has a step in which the
  member calls `todo-web-app` (CC-DECIDED-BY-ACTOR-STEP); `n3` is a `sendSignal` to the
  member saying the title is unchanged; the commit paths end in `out-team-member` after
  the `n8` merge. `n4` keeps its web and MCP actor calls, so the commit is a third web
  stimulus (dblclick, fill, press Enter).

### Contracts

- `readOnly: true` on the operations a probe reads: `TodoListAccess.RecallList`,
  `MessageBus.Subscribe`, `ListManager.OpenList` (a browser can probe only through a
  Manager route) and every operation of the two pure Engines.

### Test plans

- `principals`: `team-member` runs as subject `alice` (role `member`), `team-agent` as
  `agent-1` (role `agent`, `onBehalfOf` `alice`: TP-PRINCIPAL-DELEGATION, Task 2.13c), on
  every plan whose Client an actor calls.
- Browser steps are `{kind, model}` UIAction envelopes over design element ids; the
  observations are UIObservable envelopes. `MarkTodo` is a `check` (it was a `click`),
  so the generated spec asserts `toBeChecked(immediate)` under injected latency.
- The web plan's reword scenarios are P1 (abandon: dblclick, Escape) and P2/P3 (commit:
  dblclick, fill, press Enter). The harvest bound P2/P3 to one `fill` that never
  committed anything.
- Arrange: the harvest relied on hook-seeded state. Go plans now build it with
  `todoListAccess.RecordTodo`/`SetCompletion` contract calls; browser plans, which can call
  only Manager routes, build it with `ListManager.AddTodo` and `ToggleAll`.
- Probes for what an actor is shown: each new `sendSignal` is an expected output of the last
  stimulus open (TP-STATE) and of the actor's stimulus (TP-ACTOR-OBSERVED), so the plan that
  owns that stimulus carries a probe `for` the signal node. They are copies of the
  binding's own state read (`RecallList`, `Subscribe`, `OpenList`). A pure Engine has nothing
  to read but its own operation, so it probes itself, with `before` equal to the expectation
  (the vacuity check needs a declared first state).

### Not changed / known limits

- The dialect has no type-and-submit: add-a-todo and the work-the-list steps keep a bare
  `fill` and the post-commit observables the harvest recorded. The first run of the
  generated Playwright on a built app decides whether that needs a `press Enter` step.
- The harvest's work-the-list web steps continue from one another's state without arrange.
- Elements that exist only when the list is non-empty or on a notice are marked `repeats`:
  it is the design's only way to say "not on an empty page" for the surface check.
- The `#/active` and `#/completed` screens carry no elements of their own (ids are unique
  in a design and every element lives on `#/`); they exist so `url` observations resolve.
- The uiDesign `content` (narrative nothing reads) is unchanged. Its principle P3, no optimistic
  mutation, now contradicts Layer A, which asserts a checkbox shows its new state at once; the
  contradiction is the harvest's `server-confirmed state` question from spec section 8, not
  settled here.
- testgen warns that reword-a-todo-P1's `n3` output is Immediate and no observable is joined
  to it; the join is not implemented.

## Rebinding for the strong proof (FPC plan 2, Task 2.13c)

Run over the harvested bodies, the 2.13 bindings failed for reasons the null build hid. Every
change, by cause:

- Engines carry `expect` only. A pure Engine's step probed its own operation with its own inputs
  (TP-PROBE-SELF), so those probes are gone; a step on a read-only operation that asserts a result
  discharges the outputs it reaches (TP-STATE), and its falsifiability is the null run's.
- The store is shared by every scenario of a package. `todoListAccess` scenarios assumed seeded
  state that no longer exists (no hooks): each builds it with `RecordTodo`/`SetCompletion`
  arranges (an operation that is the scenario's own stimulus cannot be arranged, TP-HOOK, so
  `add-a-todo-P1` starts from an empty list) and the `RecallList` read steps no longer probe
  themselves. The three chains (`drive-the-list-from-agent-tooling-P1`, `work-the-list-P1/P2`)
  remove the todo that exists (`true`), then complete the removed one (refused, `NotFound`); the
  last step's probe names the owner by the step's own result, since the list is empty before and
  after it. The cause of the "arrange does not take effect" failures in the `listManager` plan was
  not owner mismatch but shared state: `RecordTodo` is `ON CONFLICT DO NOTHING`, so a todo id (and
  list) left by an earlier scenario won. Every `listManager` binding now opens by emptying
  `alice` and `bob` (`SetAllCompletion(true)` then `PurgeCompleted`).
- A `listManager` stimulus is called by a Client, not an actor, so the plan's principals did not
  apply and every step ran anonymous (the access engine refused it as `incompleteQuestion`). The
  plan now names who each Client acts for, keyed by the caller as the derivation writes it
  (`todo-web-app`, `todo-mcp-server`, `todo-api`): all three run as `alice`. The harvested
  ListManager reads its agent's delegation from `Claims["delegations"]` (grantor and intents).
  The scenario host (FPC plan 3, Task 3.0) now writes the claim in exactly that shape: a
  principal with `onBehalfOf` carries one grant `{grantor, intents}` built from `onBehalfOf` and
  the new `TestPrincipal.intents`, and `onBehalfOf` without intents has no reach (a grant with no
  intents permits nothing). The `team-agent` principal of the machine clients (`todoApi`,
  `todoMcpServer`) declares `onBehalfOf` `alice` and the seven `Intent` values its scenarios use
  (`read`, `add`, `reword`, `discard`, `mark`, `toggleAll`, `clearCompleted`). The Manager
  scenarios still drive the agent surfaces as the delegating member.
- `messageBus`: the `Subscribe` probes stay (the contract has no non-blocking read) and are
  bounded; their pre-state read times out and counts as not held. `resume-the-list-P3` (a
  `Subscribe` step) arranges one `Publish` first so it returns, and drops the probes that repeated
  its own call.
- `listManager` `add-a-todo-P2` (a refused add changes nothing) declares the list as its `before`
  state in place of the vacuity check.
- Contract and body adaptation (the one change to a harvested body): the harvest answered a
  refused title (`accepted:false,title:""`) and an all-completed toggle (`markCompleted:false`)
  with the zero value, which a null subject also returns, so no binding could tell the ruling from
  its absence. `ListRulingEngine`'s `TitleRuling` gains `reason` (`blank` when refused) and
  `ToggleTarget` gains `basis` (`someActive` / `allCompleted`); the engine reports them and the
  bindings assert them. The final tally of `work-the-list-P1` is bound over a list with one
  active todo (count 1), not zero.

## Harvested implementation bodies (`impl/`, FPC plan 2, Task 2.13b)

`impl/` holds the RUN-021 harvest's hand-written component bodies, the non-generated,
non-test `.go` files of `harvest/app/server/internal/**` and `cmd/server/hooks.go`
(list-manager, list-ruling and access-granting engines, todo-list-access, message-bus).
`conformance.WithImpl(FixtureImpl("todomvc"))` overlays them on the scaffold after the
generators have run and before the first build: each directory the overlay writes a Go file
into loses its generated `null.gen.go`, and `hooks.go` replaces nullgen's. The subtest
`TestConformance_RUN021Proof/discriminate_impl` then runs `testgen -discriminate` over the
real bodies and requires zero `TP-BROKEN` and zero survivors.

The `testdata` path keeps these files out of the platform module's build and lint.

Adaptations to the harvest (the complete list):

- The module path in every import is `example.com/conformance/todomvc` (the scaffold's), not
  the harvest's `github.com/local/todomvc-run-20261105T141816Z-04ce59ca`.
- `cmd/server/hooks.go` gains `MCPMount` (and the `mcp` import): the generated composition
  root now asks the app for it, and the harvest predates the seam. It returns the
  authenticated handler unchanged.
- Plan 3 Task 3.4: `cmd/server/hooks.go` loses `PolicyDecisionPoint`, `listVerbs` and
  `listPolicy`. The composition root's authorization policy is generated from the model
  (`policy.gen.go`: an authenticated principal may take exactly the verbs the generated transports
  send, on its own `listCatalog` scope); whose list an operation touches stays the ListManager's
  decision through AccessGrantingEngine.
- Task 2.13c: `engine/listruling/listrulingengine.go` reports the new `TitleRuling.reason` and
  `ToggleTarget.basis` (see "Rebinding for the strong proof"); the matching fields are added to the
  fixture's `listRulingEngine` contract.
- Nothing else is changed; no interface of a component moved between binding model v1 and v2.

## Review follow-ups (Task 2.13c, round 1)

- `TitleRuling.reason` (`"" | "blank"`) and `ToggleTarget.basis` (`someActive | allCompleted`) are
  declared as `enum` in `serviceContracts.listRulingEngine.$defs`, like `View` and `Intent`.
- Owed to the app (Task 2.11's follow-up; this task does not edit the app): the `$defs` mirror of
  `TestPrincipal.onBehalfOf`, an `enum` (`agent`) for `methodcheck.Actor.kind`, the two agent
  principals in archistrator's own `project.json`, and the prompt updates for TP-EXPECT-ZERO,
  TP-PRINCIPAL-DELEGATION, TP-PROBE-SELF and the relaxed TP-STATE.
- Needs a lead ruling: TP-STATE is relaxed for any read-only operation whose step asserts a result
  or error (Managers and ResourceAccess reads as well as Engines), while decision 2 scoped the
  probe-less proof to pure Engines. Narrowing it to Engine-layer contracts plus ResourceAccess reads
  is the alternative. The `ListRulingEngine` body and contract change (`reason`, `basis`) widens the
  plan's "fix the binding or the harness, never the body"; TP-EXPECT-ZERO is the justification.
- `TestGo_TypeChecksAgainstTheRealHost` (framework-go-app-generator/testgen) fails for a cause that
  is not the module cache: the realHost module replaces only `../framework-go-scenariohost`, whose
  HEAD consumes `projectmodel.Substrate`, `SubstrateTemporal` and `InfraEnvName`, which the released
  framework-go-projectmodel v0.4.0 lacks. It passes only after projectmodel is released and pinned
  (the Step-3 release), or once `moduleOpts{realHost: true}` also replaces framework-go-projectmodel
  with the local tree.
