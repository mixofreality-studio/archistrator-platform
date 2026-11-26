package methodcheck

import (
	"slices"
	"strings"

	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
)

// prerequisites.go is what every rule needs recorded before it can judge anything (RUN-025).
//
// A rule reads artifacts. Until every one of them is recorded the rule has nothing to judge, and a
// finding it raises anyway names an artifact the Method has not reached yet as a defect of the slot
// being recorded. RUN-025: the scenario-derivation rules (SCN-UNADDRESSED, SCN-ACTOR-PATH-CLIENT)
// hold an actor's input to reaching a contracted Client. They ran whenever the core use cases were
// committed, and passed vacuously only while no node was linked to an actor; once UC-ACTOR-DOOR
// required those links, the coreUseCases slot could not be recorded at all, because no Client, and
// no contract, exists before the architecture and construction record them.
//
// So every rule declares its prerequisites here, and ValidateProject keeps a rule's findings only
// once all of them are recorded (withPrerequisitesRecorded). Once they are, the rule runs. A rule
// whose subject is a record of its own (a test plan, a UI design, an acceptance plan) declares the
// artifacts upstream of that record; the record's absence already means there is nothing to judge.
//
// A second tier, ruleFullSeverity, is what a rule needs recorded to hold its findings at Error. The
// contracts are recorded one component at a time, so a rule that reads the whole contract set sees
// the components still to come as uncontracted. Such a rule runs on the first contract (its
// prerequisites are recorded) but reports as Warning, annotated with the ids still owing a contract,
// until RequiresContractsSettled holds (downgradeUntilFullyRecorded). The registry carries the window,
// so the record verb and the app's gate (Check, via GateFindings) judge the same findings.
//
// DISCIPLINE: a new RuleID constant gets its entry in the same change. The registry test
// (TestRulePrerequisites_EveryRuleDeclaresThem) fails on a constant with none.

// Prerequisite is one artifact a rule needs recorded. Each variant names its artifact and says
// whether a project records it; the interface is sealed by its unexported methods, so a new
// variant cannot be added without both.
//
//sumtype:decl
type Prerequisite interface {
	artifact() string
	recordedIn(p Project) bool
}

// RequiresMission: the committed mission (slot 0).
type RequiresMission struct{}

// RequiresGlossary: the committed glossary (slot 1).
type RequiresGlossary struct{}

// RequiresScrubbedRequirements: the committed required behaviors (slot 2).
type RequiresScrubbedRequirements struct{}

// RequiresVolatilities: the committed volatilities (slot 3).
type RequiresVolatilities struct{}

// RequiresCoreUseCases: the committed core use cases (slot 4).
type RequiresCoreUseCases struct{}

// RequiresSystem: the committed System, the architecture (slot 5).
type RequiresSystem struct{}

// RequiresOperationalConcepts: the committed deployment and operations model (slot 6).
type RequiresOperationalConcepts struct{}

// RequiresStandardCheck: the committed standard check (slot 7).
type RequiresStandardCheck struct{}

// RequiresContracts: at least one service contract is recorded (.serviceContracts). Contracts are
// recorded one component at a time during construction; a rule that reads the set runs from the
// first one. While the set is incomplete the rules of the full-severity tier (ruleFullSeverity)
// report as Warning, because the components still to come read as uncontracted.
type RequiresContracts struct{}

// RequiresContractsSettled: no contract-bearing component is both uncontracted and still to be
// constructed (ContractsStillToCome is empty). A component whose construction activity has
// completed (or that no activity covers) and that still has no contract is a defect of the finished
// design, so it closes the window: the signal is the activity's own completion, never the fact the
// rules check. Vacuously recorded while no System is committed.
type RequiresContractsSettled struct{}

// RequiresTestPlan: at least one component test plan is recorded (.phaseArtifacts.testPlan).
type RequiresTestPlan struct{}

// RequiresUIDesign: at least one surface's UI design is recorded (.phaseArtifacts.uiDesign).
type RequiresUIDesign struct{}

func (RequiresMission) artifact() string              { return "mission" }
func (RequiresGlossary) artifact() string             { return "glossary" }
func (RequiresScrubbedRequirements) artifact() string { return "scrubbedRequirements" }
func (RequiresVolatilities) artifact() string         { return "volatilities" }
func (RequiresCoreUseCases) artifact() string         { return "coreUseCases" }
func (RequiresSystem) artifact() string               { return "system" }
func (RequiresOperationalConcepts) artifact() string  { return "operationalConcepts" }
func (RequiresStandardCheck) artifact() string        { return "standardCheck" }
func (RequiresContracts) artifact() string            { return "serviceContracts" }
func (RequiresContractsSettled) artifact() string {
	return "serviceContracts for every contract-bearing component whose construction is still to come"
}
func (RequiresTestPlan) artifact() string { return "phaseArtifacts.testPlan" }
func (RequiresUIDesign) artifact() string { return "phaseArtifacts.uiDesign" }

func (RequiresMission) recordedIn(p Project) bool      { return slotRecorded(p, kindMission) }
func (RequiresGlossary) recordedIn(p Project) bool     { return slotRecorded(p, kindGlossary) }
func (RequiresVolatilities) recordedIn(p Project) bool { return slotRecorded(p, kindVolatilities) }
func (RequiresCoreUseCases) recordedIn(p Project) bool { return slotRecorded(p, kindCoreUseCases) }
func (RequiresSystem) recordedIn(p Project) bool       { return slotRecorded(p, kindSystem) }
func (RequiresStandardCheck) recordedIn(p Project) bool {
	return slotRecorded(p, kindStandardCheck)
}
func (RequiresScrubbedRequirements) recordedIn(p Project) bool {
	return slotRecorded(p, kindScrubbedRequirements)
}
func (RequiresOperationalConcepts) recordedIn(p Project) bool {
	return slotRecorded(p, kindOperationalConcepts)
}
func (RequiresContracts) recordedIn(p Project) bool { return len(p.ServiceContracts) > 0 }
func (RequiresContractsSettled) recordedIn(p Project) bool {
	return len(ContractsStillToCome(p)) == 0
}
func (RequiresTestPlan) recordedIn(p Project) bool {
	return p.PhaseArtifacts != nil && len(p.PhaseArtifacts.TestPlan) > 0
}
func (RequiresUIDesign) recordedIn(p Project) bool {
	return p.PhaseArtifacts != nil && len(p.PhaseArtifacts.UIDesign) > 0
}

// PrerequisiteName is the artifact pr names, as project.json spells it.
func PrerequisiteName(pr Prerequisite) string { return pr.artifact() }

// IsContractBearing reports whether construction owes component c a contract: the one definition
// shared with projectmodel and the app generator (projectmodel.SystemComponent.OwesContract), so
// every consumer counts the same set. A Client is contract-bearing; a Resource and a component
// nobody builds here (buildStatus planned or external) are not.
func IsContractBearing(c Component) bool {
	return projectmodel.SystemComponent{Kind: c.Kind, BuildStatus: c.BuildStatus}.OwesContract()
}

// UncontractedComponents is the id of every contract-bearing component of the committed System
// that has no contract recorded, in System order. Nil when no System is committed or none is owed.
func UncontractedComponents(p Project) []string {
	sys, ok, err := p.system()
	if err != nil || !ok {
		return nil
	}
	return uncontracted(p, sys)
}

func uncontracted(p Project, sys System) []string {
	keys := map[string]bool{}
	for k := range p.ServiceContracts {
		keys[scenario.Normalize(k)] = true
	}
	var out []string
	for _, c := range sys.Components {
		if !IsContractBearing(c) {
			continue
		}
		if keys[scenario.Normalize(c.ID)] || (c.ContractKey != "" && keys[scenario.Normalize(c.ContractKey)]) {
			continue
		}
		out = append(out, c.ID)
	}
	return out
}

// ContractsStillToCome is the id of every uncontracted contract-bearing component whose
// construction is yet to complete: the components whose contract may legitimately still arrive,
// which keep the contracts-pending window open. Construction progress is the one the alignment
// check reads (componentProgress and withGeneratedClientsPending): a component is pending until a
// covering activity completes. Once it has completed, a missing contract is a defect, so the
// component leaves this list and the rules that read the contract set hold Error again.
func ContractsStillToCome(p Project) []string {
	sys, ok, err := p.system()
	if err != nil || !ok {
		return nil
	}
	owing := uncontracted(p, sys)
	if len(owing) == 0 {
		return nil
	}
	pending, _, err := p.componentProgress()
	if err != nil {
		return nil
	}
	withGeneratedClientsPending(sys, pending)
	return slices.DeleteFunc(owing, func(id string) bool { return !pending[id] })
}

// recorded reports whether p records the artifact pr names: a committed slot of its kind, or a
// non-empty top-level record.
func recorded(p Project, pr Prerequisite) bool { return pr.recordedIn(p) }

func slotRecorded(p Project, kind int) bool {
	_, ok := p.slotByKind(kind)
	return ok
}

// RulePrerequisites is what rule id needs recorded before it runs; ok is false for an id the
// registry does not know.
func RulePrerequisites(id RuleID) (prs []Prerequisite, ok bool) {
	prs, ok = rulePrerequisites[id]
	return slices.Clone(prs), ok
}

// RuleFullSeverityPrerequisites is what rule id needs recorded, beyond RulePrerequisites, to keep
// Error severity; ok is false for a rule with no such tier.
func RuleFullSeverityPrerequisites(id RuleID) (prs []Prerequisite, ok bool) {
	prs, ok = ruleFullSeverity[id]
	return slices.Clone(prs), ok
}

// downgradeUntilFullyRecorded turns the Error findings of a full-severity-tier rule into Warnings
// while the rule's full-severity prerequisites are not all recorded.
func downgradeUntilFullyRecorded(p Project, fs []Finding) []Finding {
	pending := ContractsStillToCome(p)
	if len(pending) == 0 {
		return fs
	}
	note := " [advisory: contracts are still being recorded (none yet for " + strings.Join(pending, ", ") +
		"); this rule regains Error severity once the construction of each has completed or its contract is recorded]"
	out := make([]Finding, len(fs))
	for i, f := range fs {
		out[i] = f
		if f.Severity != SeverityError {
			continue
		}
		prs, tiered := ruleFullSeverity[f.RuleID]
		if !tiered || allRecorded(p, prs) {
			continue
		}
		out[i].Severity = SeverityWarning
		out[i].Message += note
	}
	return out
}

func allRecorded(p Project, prs []Prerequisite) bool {
	for _, pr := range prs {
		if !recorded(p, pr) {
			return false
		}
	}
	return true
}

// prerequisitesRecorded reports whether p records every prerequisite of id. An id the registry
// does not know (a scenario Problem code methodcheck has no rule for) is never held back: an
// unknown rule is reported, not silenced.
func prerequisitesRecorded(p Project, id RuleID) bool {
	prs, ok := rulePrerequisites[id]
	if !ok {
		return true
	}
	for _, pr := range prs {
		if !recorded(p, pr) {
			return false
		}
	}
	return true
}

// withPrerequisitesRecorded keeps the findings of the rules whose prerequisites p records.
func withPrerequisitesRecorded(p Project, fs []Finding) []Finding {
	ready := map[RuleID]bool{}
	out := fs[:0:0]
	for _, f := range fs {
		ok, seen := ready[f.RuleID]
		if !seen {
			ok = prerequisitesRecorded(p, f.RuleID)
			ready[f.RuleID] = ok
		}
		if ok {
			out = append(out, f)
		}
	}
	return out
}

// The prerequisite sets the families share.
var (
	onMission       = []Prerequisite{RequiresMission{}}
	onGlossary      = []Prerequisite{RequiresGlossary{}}
	onScrubbed      = []Prerequisite{RequiresScrubbedRequirements{}}
	onVolatilities  = []Prerequisite{RequiresVolatilities{}, RequiresGlossary{}, RequiresScrubbedRequirements{}}
	onUseCases      = []Prerequisite{RequiresCoreUseCases{}}
	onSystem        = []Prerequisite{RequiresSystem{}}
	onArchitecture  = []Prerequisite{RequiresCoreUseCases{}, RequiresSystem{}}
	onDeployment    = []Prerequisite{RequiresOperationalConcepts{}, RequiresMission{}, RequiresSystem{}}
	onStandardCheck = []Prerequisite{RequiresStandardCheck{}}
	onContracts     = []Prerequisite{RequiresCoreUseCases{}, RequiresSystem{}, RequiresContracts{}}
	onTestPlan      = []Prerequisite{RequiresTestPlan{}}
	onUIDesign      = []Prerequisite{RequiresUIDesign{}}
)

// ruleFullSeverity is the second tier: the rules that read the whole contract set, which run once
// their rulePrerequisites are recorded but hold Error severity only once every contract-bearing
// component has a contract. A new entry names a rule that rulePrerequisites also declares.
var ruleFullSeverity = map[RuleID][]Prerequisite{
	ruleSCNUnaddressed:     {RequiresContractsSettled{}},
	ruleSCNActorPathClient: {RequiresContractsSettled{}},
	ruleTPClientContracted: {RequiresContractsSettled{}},
}

// rulePrerequisites is every rule's prerequisites, grouped by the slot at which the rule first
// runs. An entry with an empty list would declare a rule that reads nothing recorded.
var rulePrerequisites = func() map[RuleID][]Prerequisite {
	m := map[RuleID][]Prerequisite{}
	add := func(prs []Prerequisite, ids ...RuleID) {
		for _, id := range ids {
			m[id] = prs
		}
	}

	// ---- glossary, required behaviors, volatilities (slots 1-3) ----
	add(onGlossary, ruleGlossFourQ)
	add(onScrubbed, ruleSRIDUnique)
	// validateVolatilities reads the glossary and required behaviors it traces into.
	add(onVolatilities, ruleVolTrace, ruleVolGloss, ruleVolAxis, ruleVolNOB, ruleVolAxisExplicit)

	// ---- core use cases (slot 4): the diagrams alone ----
	add(onUseCases,
		ruleCucCard, ruleUcActDiagram, ruleCucNameUniq, ruleCucActorUniq, ruleCucActorReq, ruleCucActorID,
		ruleUcNodeIDUniq, ruleUCActPresent, ruleUCGuardLabel, ruleUCVariationRef,
		ruleUCPathOutcome, ruleUCPathOutcomeDistinct, ruleUCOutcomeToAsker, ruleUCStartNotAction, ruleUCActorDoor, ruleUCActorUnserved,
		// A start family that is the tail of an event entry's path is redundant by the diagram's
		// shape alone (a Warning, so the architect hears it while the draft is still theirs).
		ruleUCStartRedundant)

	// ---- architecture (slot 5): the System, joined to the use cases ----
	add(onArchitecture,
		ruleSysNoUp, ruleSysNoSide, ruleSysNoSkip, ruleSysPubOrig, ruleSysPubDest, ruleSysDontMtoM, ruleSysDontCli,
		ruleSysCardMgr, ruleSysCardRatio, ruleSysCardTotal, ruleArchChainCov, ruleSysNameUniq,
		ruleUseCaseDynamicMissing, ruleSystemLayerDegenerate,
		ruleSysRAOrphan, ruleSysEncapsulates, ruleSysRelDup, ruleSysClientSurface, ruleSysClientOpLabel, ruleSysClientMachineCall,
		ruleDVEdgeEnds, ruleDVEdgeInModel, ruleDVSingleMgr, ruleDVMode, ruleDVKeyUnique,
		ruleDVStaticCoverage, ruleDVRelCoverage, ruleDVPlannedSkipped, ruleDVRelUtilityExempt, ruleCCEdgeRealized,
		ruleCCViewUseCase, ruleCCStepNode, ruleCCStepUnique, ruleCCCoverage, ruleCCStepNonempty,
		ruleCCEndpoint, ruleCCActorEdge, ruleCCActorLane, ruleCCTriggerEvent, ruleCCPathConnected,
		ruleCCDecidedBy, ruleCCDecidedByActorStep,
		ruleUCIOKinds, ruleUCNoIO, ruleUCOutcomeAtAskingSurface, ruleUCLaneResolves)
	// The Appendix-C graph rules and the code-walk rules read the System alone.
	add(onSystem,
		ruleAppcDontClientMultiMgr, ruleAppcDontMgrMultiQueue, ruleAppcDontEngineQueue,
		ruleAppcDontRAQueue, ruleAppcDontClientPub, ruleAppcDontEnginePub,
		ruleAppcDontRAPub, ruleAppcDontResourcePub, ruleAppcDontNonMgrSub,
		ruleAppcArchOpen, ruleAppcArchSemiOpen, ruleAppcCardSubMgr,
		ruleAppcSvcSingle, ruleAppcSvcStrive, ruleAppcSvcAvoid12, ruleAppcSvcReject20,
		ruleAlignMissingPkg, ruleAlignExtraPkg, ruleAlignLayerMismate,
		ruleAlignStalePlanned, ruleAlignExternalNonUtility, ruleAlignExternalUnwired,
		ruleCodeEdgeNotInModel, ruleModelEdgeNotInCode,
		ruleGenSurfaceSupported)
	add(append(slices.Clone(onSystem), onMission...), ruleSysSurfaceRequired)
	add(append(slices.Clone(onArchitecture), onMission...), ruleSysSurfaceEntry)

	// ---- deployment and operations (slot 6), against the mission and the System ----
	add(onDeployment,
		ruleOpcObjRef, ruleOPCTopicCoverage,
		ruleDepContainerRef, ruleDepMemberExist, ruleDepProfileSet, ruleDepGraphIdentity, ruleDepCoverage,
		ruleDepNodeWellformed, ruleDepContainerUsed, ruleDepMemberExclusive, ruleDepResourcePresent,
		ruleDepPlannedSkipped, ruleDepKeyUnique, ruleDepEdgeRef, ruleDepEdgeIsolated,
		ruleDepFrontendPresent, ruleDepEdgeGateway)

	// ---- standard check (slot 7) ----
	add(onStandardCheck, ruleStdWaive, ruleStdStatusExplicit, ruleStdFailOpen)

	// ---- service contracts (construction): the derived scenarios against the contract surface ----
	add(onContracts,
		ruleSCNUnaddressed, ruleSCNActorPathClient,
		ruleTPOpReached, ruleTPClientContracted, ruleTPLabelOp, ruleTPClientOps, ruleClientMachineMirror)

	// ---- UI designs ----
	add(onUIDesign, ruleUIDRole, ruleUIDName, ruleUIDIDUnique, ruleUIDWithin, ruleUIDRoute)

	// ---- test plans and the acceptance plan: each judges its own record against the derived
	// scenarios and the contracts, which are upstream of every plan ----
	add(onContracts,
		ruleTPBound, ruleTPStep, ruleTPStepOp, ruleTPArgName, ruleTPArgType,
		ruleTPExpect, ruleTPProbe, ruleTPState, ruleTPSkip,
		ruleTPHook, ruleTPExpectRequired, ruleTPArgRef, ruleTPArrange, ruleTPActorObserved, ruleTPPrincipal,
		ruleTPProbeSelf, ruleTPProbePolling, ruleTPPrincipalDelegation, ruleTPExpectZero, ruleTPNullSatisfied,
		ruleTPUITarget, ruleTPUIAction,
		ruleATPBound, ruleATPRef, ruleATPBrowser, ruleATPProbeAs)
	// TP-KEY's defect is a plan filed under no contract key, including one filed before any
	// contract exists: the plan alone is what it needs.
	add(onTestPlan, ruleTPKey)
	return m
}()
