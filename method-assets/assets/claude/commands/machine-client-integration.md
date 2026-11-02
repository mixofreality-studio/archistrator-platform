# /machine-client-integration

> The whole activity of a machine client (a REST API or an MCP server): commit what the generators wrote and pass its gate. A machine client authors no code, no contract and no plan.

**Arguments** — `$ARGUMENTS` is `<component_id> <activity_id>` (two space-separated tokens). Parse once; do not swap them. Work lands on the shared activity branch `activity/<activity_id>`.

**Agent + skills.** Work to the standard of the **`system-architect`** agent (`.claude/agents/system-architect.md`). Follow **[[the-method-layers]]** and **[[the-method-project-state]]**.

**Goal / intention.** The job has already run `go tool appgen`, so this client's routes or tools, the composition root and the client's generated proof (`client_scenarios.gen_test.go`) are on disk. That proof drives every route or tool to the Manager operation behind it and checks the actor sees the result and a refusal. Business rules are the Manager's scenarios to prove, not this client's. Nothing here is yours to write except the composition root's hooks, and there is no contract or test plan to record.

**Checks.** Call `run_checks()` until it reports every check passing, and finish with one full `run_checks()` call. Do not run the checks yourself.

## Steps

> **Operator notes first.** First call `getOperatorNotes`; if it returns notes, act on them before anything else. They are the operator's steer for this attempt (a send-back, retry or re-queue note) and outrank the defaults below.

1. **Fill the hooks, if any are open.** The job seeds `cmd/server/hooks.go` once, and that file is yours: replace every `FILL` panic in it with the app's own answer (the authorization policy, an external service's URL). Never edit a generated file, and do not write a test.
2. **Verify.** Call `run_checks()`. A red generated proof is a real defect. The generated file cannot be repaired here: if the hooks are not its cause, say what failed in your integration note and `publishDraft`; the gate stays red and the review sees it.
3. **Commit** what the generators wrote and your hooks onto `activity/<activity_id>`.
4. **Record** a short integration note via `recordPhaseArtifact` per [[the-method-project-state]], then `publishDraft`.
5. **Stop.** Do not mark phase status (the Manager owns that) and do not merge.
