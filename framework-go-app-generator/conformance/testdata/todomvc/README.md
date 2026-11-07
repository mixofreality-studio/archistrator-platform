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

Fixtures live under `conformance/testdata/` (not `testdata/conformance/`)
because `go:embed` cannot reference parent directories.

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
  `agent-1` (role `agent`), on every plan whose Client an actor calls. TestPrincipal has
  no delegation claims, so the agent's delegation from `alice` is not expressed; the
  agent happy paths will be Refused at run time until the principal can carry it.
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
- Nothing else is changed; no interface of a component moved between binding model v1 and v2.
