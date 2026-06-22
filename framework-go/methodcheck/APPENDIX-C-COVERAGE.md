# Appendix-C Coverage Matrix

Machine-readable table: see `coverage.go` `DefaultCoverage()`.
Generated rule IDs are stable — CI and diagnostics reference them by ID.

| AppcRef | Kind | Classification | RuleID | Book text (excerpt) |
|---------|------|----------------|--------|---------------------|
| PRIME | directive | human-judgment | — | Never design against the requirements |
| DIR-1 | directive | human-judgment | — | Avoid functional decomposition |
| DIR-2 | directive | human-judgment | — | Decompose based on volatility |
| DIR-3 | directive | human-judgment | — | Provide a composable design |
| DIR-4 | directive | human-judgment | — | Offer features as aspects of integration |
| DIR-5 | directive | human-judgment | — | Design iteratively, build incrementally |
| DIR-6 | directive | human-judgment | — | Design the project to build the system |
| DIR-7 | directive | human-judgment | — | Drive educated decisions with viable options |
| DIR-8 | directive | human-judgment | — | Build along the critical path |
| DIR-9 | directive | human-judgment | — | Be on time throughout the project |
| SYS-1a | guideline | human-judgment | — | Capture required behaviour |
| SYS-1b | guideline | human-judgment | — | Describe behaviour with use cases |
| SYS-1c | guideline | automated-design | UC-ACTDIAG | Document nested conditions with activity diagrams |
| SYS-1d | guideline | human-judgment | — | Eliminate solutions masquerading as requirements |
| SYS-1e | guideline | automated-design | ARCH-CHAINCOV | Validate system supports all core use cases |
| SYS-2a | guideline | automated-design | SYS-CARD-MGR | Avoid more than five Managers |
| SYS-2b | guideline | human-judgment | — | Avoid more than a handful of subsystems |
| SYS-2c | guideline | automated-design | APPC-CARD-SUB-MGR | Avoid more than three Managers per subsystem |
| SYS-2d | guideline | automated-design | SYS-CARD-RATIO | Strive for golden ratio of Engines to Managers |
| SYS-2e | guideline | human-judgment | — | RA components may access more than one Resource |
| SYS-3a | guideline | human-judgment | — | Volatility decreases top-down |
| SYS-3b | guideline | human-judgment | — | Reuse increases top-down |
| SYS-3c | guideline | human-judgment | — | Do not encapsulate nature of business |
| SYS-3d | guideline | human-judgment | — | Managers should be almost expendable |
| SYS-3e | guideline | human-judgment | — | Design should be symmetric |
| SYS-3f | guideline | human-judgment | — | Never use public channels for internal interactions |
| SYS-4a | guideline | automated-design | APPC-ARCH-OPEN | Avoid open architecture |
| SYS-4b | guideline | automated-design | APPC-ARCH-SEMI-OPEN | Avoid semi-closed/semi-open architecture |
| SYS-4c-i | guideline | automated-design | SYS-NOUP | Do not call up |
| SYS-4c-ii | guideline | automated-design | SYS-NOSIDE | Do not call sideways |
| SYS-4c-iii | guideline | automated-design | SYS-NOSKIP | Do not call more than one layer down |
| SYS-4c-iv | guideline | human-judgment | — | Resolve opens via queued/async |
| SYS-4d | guideline | human-judgment | — | Extend system by implementing subsystems |
| SYS-5a | guideline | automated-design | APPC-INT-UTILITY | All components can call Utilities |
| SYS-5b | guideline | automated-design | APPC-INT-MGR-ENG-RA | Managers and Engines can call ResourceAccess |
| SYS-5c | guideline | automated-design | APPC-INT-MGR-ENG | Managers can call Engines |
| SYS-5d | guideline | automated-design | SYS-NOSIDE | Managers can queue calls to another Manager |
| SYS-6a | directive | automated-design | APPC-INT-CLIENT-MULTI-MGR | Clients do not call multiple Managers per use case |
| SYS-6b | directive | automated-design | APPC-INT-MGR-MULTI-QUEUE | Managers do not queue to >1 Manager per use case |
| SYS-6c | directive | automated-design | APPC-INT-ENGINE-NO-QUEUE | Engines do not receive queued calls |
| SYS-6d | directive | automated-design | APPC-INT-RA-NO-QUEUE | ResourceAccess do not receive queued calls |
| SYS-6e | directive | automated-design | APPC-INT-CLIENT-NO-PUB | Clients do not publish events |
| SYS-6f | directive | automated-design | APPC-INT-ENGINE-NO-PUB | Engines do not publish events |
| SYS-6g | directive | automated-design | APPC-INT-RA-NO-PUB | ResourceAccess do not publish events |
| SYS-6h | directive | automated-design | APPC-INT-RESOURCE-NO-PUB | Resources do not publish events |
| SYS-6i | directive | automated-design | APPC-INT-NONMGR-NO-SUB | Engines/RA/Resources do not subscribe to events |
| PROJ-1a | guideline | human-judgment | — | Design project to build system |
| PROJ-1b | guideline | human-judgment | — | Re-design project on each scope change |
| PROJ-1c | guideline | human-judgment | — | Use activities as procurement units |
| PROJ-1d | guideline | human-judgment | — | Activities represent design/build/test |
| PROJ-1e | guideline | human-judgment | — | Use correct activity types |
| PROJ-1f | guideline | human-judgment | — | Architect designs project |
| PROJ-1g | guideline | human-judgment | — | Use time-based estimates |
| PROJ-2a | guideline | human-judgment | — | Assign architect to all critical-path activities |
| PROJ-2b | guideline | human-judgment | — | Use small teams |
| PROJ-2c | guideline | human-judgment | — | Match seniority to activity type |
| PROJ-2d | guideline | human-judgment | — | Avoid one developer per component |
| PROJ-2e | guideline | human-judgment | — | Cross-train developers |
| PROJ-2f | guideline | human-judgment | — | Assign testers early |
| PROJ-2g | guideline | human-judgment | — | Strive for 1:1–2:1 tester-to-developer ratio |
| PROJ-2h | guideline | human-judgment | — | Include QA engineer |
| PROJ-3a | guideline | human-judgment | — | Integrate continuously |
| PROJ-3b | guideline | human-judgment | — | Avoid big-bang integration |
| PROJ-4a | guideline | human-judgment | — | Use effort-days for estimates |
| PROJ-4b | guideline | human-judgment | — | Include management overhead |
| PROJ-4c | guideline | human-judgment | — | Include testing in estimates |
| PROJ-4d | guideline | human-judgment | — | Include integration in estimates |
| PROJ-4e | guideline | human-judgment | — | Include QA in estimates |
| PROJ-4f | guideline | human-judgment | — | Include documentation in estimates |
| PROJ-4g | guideline | human-judgment | — | Revisit estimates at design review |
| PROJ-5a | guideline | human-judgment | — | Use CPM network |
| PROJ-5b | guideline | human-judgment | — | Make dependencies explicit |
| PROJ-5c | guideline | human-judgment | — | Distinguish hard vs. soft dependencies |
| PROJ-5d | guideline | human-judgment | — | Compute critical path |
| PROJ-5e | guideline | human-judgment | — | Place high-risk items on critical path |
| PROJ-5f | guideline | human-judgment | — | Use milestones sparingly |
| PROJ-5g | guideline | human-judgment | — | Identify float for each activity |
| PROJ-5h | guideline | human-judgment | — | Re-compute critical path on change |
| PROJ-5i | guideline | human-judgment | — | Adjust staffing to critical path |
| PROJ-5j | guideline | human-judgment | — | Keep network in source control |
| PROJ-6a | guideline | human-judgment | — | Track earned value weekly |
| PROJ-6b | guideline | human-judgment | — | Compare schedule vs actuals |
| PROJ-6c | guideline | human-judgment | — | Report cost variance |
| PROJ-6d | guideline | human-judgment | — | Use S-curve for burn rate |
| PROJ-6e | guideline | human-judgment | — | Forecast completion from actuals |
| PROJ-6f | guideline | human-judgment | — | Escalate slippage early |
| PROJ-6g | guideline | human-judgment | — | Never compress the schedule |
| PROJ-7a | guideline | human-judgment | — | Identify risks at project design |
| PROJ-7b | guideline | human-judgment | — | Classify by impact and probability |
| PROJ-7c-i | guideline | human-judgment | — | Mitigation: avoid |
| PROJ-7c-ii | guideline | human-judgment | — | Mitigation: transfer |
| PROJ-7d | guideline | human-judgment | — | Assign risk owners |
| PROJ-7e | guideline | human-judgment | — | Review risks weekly |
| PROJ-7f | guideline | human-judgment | — | Update risk register on scope change |
| PROJ-7g | guideline | human-judgment | — | Escalate unmitigated risks |
| PROJ-7h | guideline | human-judgment | — | Close resolved risks |
| TRACK-1 | guideline | human-judgment | — | Track activities by phase completion |
| TRACK-2 | guideline | human-judgment | — | Use earned-value not percent-complete |
| TRACK-3 | guideline | human-judgment | — | Status-report weekly |
| TRACK-4 | guideline | human-judgment | — | Flag float consumption |
| TRACK-5 | guideline | human-judgment | — | Trigger re-design on >10% slippage |
| TRACK-6 | guideline | human-judgment | — | Keep network consistent with actuals |
| SVC-1 | guideline | human-judgment | — | Design reusable service contracts |
| SVC-2a | guideline | automated-contract | APPC-SVC-SINGLE | Avoid contracts with a single operation |
| SVC-2b | guideline | automated-contract | APPC-SVC-STRIVE | Strive for 3–5 operations per contract |
| SVC-2c | guideline | automated-contract | APPC-SVC-AVOID-12 | Avoid contracts with more than 12 operations |
| SVC-2d | directive | automated-contract | APPC-SVC-REJECT-20 | Reject contracts with 20 or more operations |
| SVC-3 | guideline | human-judgment | — | Avoid property-like operations |
| SVC-4 | guideline | human-judgment | — | Limit contracts per service to 1 or 2 |
| SVC-5 | guideline | human-judgment | — | Avoid junior hand-offs |
| SVC-6 | guideline | human-judgment | — | Only architect/senior designs contracts |
