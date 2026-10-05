# /frontend-integration

> The Integration step of a frontend activity: wire this UI surface into the real call chains its neighboring Managers/Engines already implement, generate its black-box flow tests from the reviewed flows, run them against the integrated surface and record the run — without touching any frozen contract or approved design.

**Arguments** — `$ARGUMENTS` is `<component_id> <activity_id>` (two space-separated tokens; `component_id` may be empty for non-component activities). Parse once; do not swap them. Work lands on the shared activity branch `activity/<activity_id>` and its single PR — you are contributing commits to a PR that already exists (or open it if this is the first phase). Do NOT open a second PR.

**Agent + skills.** Work to the standard of the **`system-architect`** agent (`.claude/agents/system-architect.md`). Follow **[[the-method-layers]]**, **[[the-method-testing]]** (§ Deterministic component scenarios) and **[[the-method-project-state]]** for all reading/updating of `.aiarch/state/project.json`.

**Goal / intention.** Per ch. 12: "features are always and everywhere aspects of integration, not implementation" — only once the pieces of a use case's call chain are each independently complete can that call chain be wired into a working feature. A frontend surface is no exception: it becomes a feature only once it is wired against the real Managers/Engines it calls, not while it is still standing alone against a mock. Per ch. 11's own worked project-design example, this is exactly how Löwy schedules Client work: Client construction happens first against simulators of the Managers it depends on, with an explicit, separate integration activity added once the real Managers are complete — because integration pressure concentrates unevenly on Manager-facing components (the same chapter shows Managers integrating with four or five other services at once) and because concurrent integration produces a nonlinear increase in complexity that lands late in the project, when there is little runway left to fix what breaks. Löwy's recommendation is to integrate only one or two services at a time. This step is also where the activity's two parallel branches meet: construction built the surface without tests while the test-engineer bound its derived flows, and here the platform generates the black-box flow tests from that reviewed plan, this step fills their hooks with the real stack, and the flows run against the integrated surface — the run is the evidence the `testing` task reviews. "Done" for this step means the call-chain edges this surface participates in are wired against the real, frozen contracts of its neighbors, replacing whatever simulator or mock it was constructed against, its generated flow tests pass, and that run is recorded. This step must NOT modify a frozen contract, the approved UI design concept, the flows (test plan) or a generated test file (a needed change to either is a scope-change or amend event, not an integration fix), must NOT add components outside the committed system design, and must NOT take on more concurrent neighbor integrations than this surface's flows actually require.

## Steps

> **Operator notes first.** First call `getOperatorNotes`; if it returns notes, act on them before anything else. They are the operator's steer for this attempt (a send-back, retry or re-queue note) and outrank the defaults below.

1. **Read what you need** from `.aiarch/state/project.json` per [[the-method-project-state]]: the activity, this surface's approved UI design concept, its Client/SPA entry (relationships in/out) in the committed system design, the frozen contracts of the specific Manager/Engine neighbors its flows cross, and the surface's reviewed flows — one test plan per contract of the surface, keyed by the contract name (`.phaseArtifacts.testPlan["<contract>"]`: the surface's own contract, e.g. `webClient` for `web-client`, and every facet whose `.serviceContracts` entry names it as `component`, e.g. `webClientBilling`). The flows are a parallel branch of this activity: if they are not committed yet, do steps 2–3, commit the wiring onto `activity/<activity_id>`, say so in the integration note, `publishDraft` and stop — generate no tests and record no run. The `testing` task joins this step and the reviewed flows, so it holds until a run of the flows exists.
2. **Wire** this surface to its integration-scope neighbors, verified against the relevant flows/dynamic view(s), and record an integration note into `.phaseArtifacts.integrationNote` via `recordPhaseArtifact` per [[the-method-project-state]].
3. **Verify** the code (fast checks, working directory `webApp`): `npm run typecheck`; `npm run lint` scoped to the files you touched for this surface and, only if the wiring directly touched them, the specific neighbor client code — not `npm run build` and not `npm run check`.
4. **Generate the tests** from the reviewed flows: `make gen-tests` (repo root). A web surface gets, per contract, Playwright specs, config and reporter under `uitests/generated/<contract>/` and a seeded `hooks.ts` beside them; an MCP client gets `<stereotype>_scenarios.gen_test.go` and a seeded `<stereotype>_hooks_test.go` in its Go package. Never edit a generated file and never change the flows.
5. **Fill the hooks.** Replace every `FILL` in the surface's hooks files (`uitests/generated/<contract>/hooks.ts` for each contract, or the MCP client's `<stereotype>_hooks_test.go`). The hooks arrange the real stack: in Go, build each collaborator through its package's public constructor (the app's own component packages, wired as production wires them); never import a package's `internal/` sub-packages, unexported identifiers or another package's test helpers, and never stand a mock or fake in for a component.
6. **Purge.** Delete every test of this surface that the generator did not write: any `*.test.ts`/`*.test.tsx` in the surface's code under `webApp/src/`, any hand-written spec for it under `uitests/` outside `uitests/generated/`, and, for an MCP client, any `_test.go` in its package other than the generated file and the hooks file. The arch gate rejects them. Then `make gen-tests-check` must be clean.
7. **Run the flows** against the integrated surface, from the repo root. Docker must be up.

   ```sh
   # <contracts>: the surface's contract names from step 1 (e.g. "webClient webClientBilling").
   if [ -d "uitests/generated/<first contract>" ]; then  # a web surface: Playwright, one config per contract
     mkdir -p test-results
     (cd uitests && npx playwright install --with-deps chromium)
     for c in <contracts>; do
       (cd uitests && npx playwright test -c "generated/${c}/playwright.config.gen.ts")
       cp -R "uitests/test-results/${c}" test-results/
     done
   else                                                  # an MCP client: Go scenarios
     make test-scenarios
   fi
   ```

   For a web surface the Playwright reporter writes `uitests/test-results/<contract>/`; the copy puts it at `test-results/<contract>/` (its `videoRef`/`traceRef` are relative to that directory). Copy it whether the run was red or green. Read the verdicts from `test-results/<contract>/results.json` for each contract: every scenario must be `pass`. Fix a red flow in the wiring, in the surface code (to its approved design and the frozen contracts) or in the hooks, and re-run. Never fix it by editing a generated test, the flows or a contract. A binding you believe is wrong goes into the integration note for the test-engineer.
8. **Commit** the wiring, the generated tests and the hooks file onto `activity/<activity_id>`. Never commit `test-results/` or `uitests/test-results/`.
9. **Record the run** with the `aiarch-state-mcp` binary's `record-test-run` subcommand (on `PATH` in the construction venue), after the commit. `--component` takes the activity's component id as you received it: the binary resolves it to every contract of the component and records one run per contract from `test-results/<contract>/results.json`. The revision is `AIARCH_REVISION`, the attempt this job runs under, which the dispatch stamps; the `testing` task matches it against the attempt it judges, so never substitute a commit SHA — if it is unset the record fails, and that failure goes into the integration note. Record the last run you made, red or green: the record is the evidence the `testing` task judges.

   ```sh
   run="${AIARCH_TEST_RUN_ID:-local-$(date -u +%Y%m%dT%H%M%SZ)}"
   artifact="${AIARCH_TEST_ARTIFACT:-.aiarch/test-results/${run}}"
   if [ -z "${AIARCH_TEST_ARTIFACT:-}" ]; then mkdir -p "${artifact}" && cp -R test-results/. "${artifact}/"; fi
   aiarch-state-mcp record-test-run --activity <activity_id> --component <component_id> \
     --run-id "${run}" --artifact "${artifact}" --results test-results \
     --revision "${AIARCH_REVISION}"
   ```

   In the cloud venue `AIARCH_TEST_RUN_ID` / `AIARCH_TEST_ARTIFACT` name the GitHub run and the artifact the job uploads `test-results/**` into; on the local venue the run is copied under `.aiarch/test-results/<run>/`.
10. **Publish.** `publishDraft` carries the integration note and the recorded run to the activity branch.
11. **Stop.** Do not mark phase status (the Manager owns that) and do not merge. Leave the PR open for the gate.
