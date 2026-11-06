# todomvc conformance fixture

Provenance: the `project.json` harvested from paid bench run RUN-021
(`run-20261105T141816Z-04ce59ca`, 2026-11-05), produced by archistrator at
commit `b31bc72f` (server v0.8.139 / webapp v0.6.122). Component and contract
ids (`todo-web-app`, `list-manager`, ...) are the harvest's as-is.

Edits applied to the harvest (the complete list):

- `id` set to `todomvc-conformance`.
- Removed top-level `activityExecution`, `testingState` and `research`.
- `phaseArtifacts` reduced to `testPlan` and `uiDesign` (dropped `srs`,
  `integrationNote`, `uxRequirements`).
- `slots`, `serviceContracts` and `reviewPolicy` kept verbatim. The harvest has
  no top-level `moduleDir` key, so none is carried.

No fixture-data fixes were needed: the stripped harvest is admissible
(projectmodel.Load, methodcheck.DecodeProject, zero Error findings from
methodcheck.ValidateProjectJSON) without further edits.

Fixtures live under `conformance/testdata/` (not `testdata/conformance/`)
because `go:embed` cannot reference parent directories.
