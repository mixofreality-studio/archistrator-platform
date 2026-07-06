package methodcheck

// emitters.go is the AUTHORITATIVE registry of every RuleID the validator can
// actually emit — one entry per rule ID a suite constructs a Finding for. It is
// the executable answer to "does this rule have an emitter?": the coverage matrix
// (DefaultCoverage) binds App-C items to rule IDs, and a platform test asserts
// every AUTOMATED coverage item's RuleID appears here. A coverage entry marked
// automated whose rule was never wired to an emitter (the SYS-5a/b/c phantom that
// motivated this registry) therefore fails the platform's own tests rather than
// silently pretending to be enforced.
//
// DISCIPLINE: when a rule func starts (or stops) emitting a rule ID, update this
// registry in the same change. Grouped by the suite that owns the emitter so the
// mapping stays reviewable.

// emittedRuleIDs returns the set of every rule ID the validator emits.
func emittedRuleIDs() map[RuleID]bool {
	ids := []RuleID{
		// ---- Phase-1 design predicates (rules.go) ----
		ruleVolTrace, ruleVolGloss, ruleVolAxis, ruleVolNOB,
		ruleCucCard, ruleUcActDiagram, ruleCucNameUniq, ruleCucActorUniq, ruleUcNodeIDUniq,
		ruleOpcObjRef,
		ruleStdWaive,

		// ---- Architecture / whole-graph (rules_system.go) ----
		ruleSysNoUp, ruleSysNoSide, ruleSysNoSkip,
		ruleSysPubOrig, ruleSysPubDest, ruleSysDontMtoM, ruleSysDontCli,
		ruleSysCardMgr, ruleSysCardRatio, ruleSysCardTotal,
		ruleArchChainCov, ruleSysNameUniq, ruleUseCaseDynamicMissing,
		ruleSystemLayerDegenerate,

		// ---- State-validation twins (rules_statevalidation.go) ----
		ruleSysRAOrphan, ruleSysEncapsulates, ruleSysRelDup, ruleDVChainConn,
		ruleUCActPresent, ruleUCGuardLabel, ruleUCVariationRef,
		ruleVolAxisExplicit, ruleStdStatusExplicit, ruleStdFailOpen,
		ruleGlossFourQ, ruleSRIDUnique, ruleOPCTopicCoverage,

		// ---- Dynamic views (rules_dynamic.go) ----
		ruleDVPartExist, ruleDVEdgeEnds, ruleDVEdgeInModel, ruleDVSingleMgr,
		ruleDVMode, ruleDVKeyUnique,
		ruleDVStaticCoverage, ruleDVRelCoverage, ruleDVPartUsed,
		ruleDVPlannedSkipped,

		// ---- Deployment (rules_deployment.go) ----
		ruleDepContainerRef, ruleDepMemberExist, ruleDepProfileSet,
		ruleDepGraphIdentity, ruleDepCoverage, ruleDepNodeWellformed,
		ruleDepContainerUsed, ruleDepMemberExclusive, ruleDepResourcePresent,
		ruleDepPlannedSkipped,

		// ---- Appendix-C (rules_appc.go) ----
		ruleAppcDontClientMultiMgr, ruleAppcDontMgrMultiQueue, ruleAppcDontEngineQueue,
		ruleAppcDontRAQueue, ruleAppcDontClientPub, ruleAppcDontEnginePub,
		ruleAppcDontRAPub, ruleAppcDontResourcePub, ruleAppcDontNonMgrSub,
		ruleAppcArchOpen, ruleAppcArchSemiOpen, ruleAppcCardSubMgr,
		ruleAppcSvcSingle, ruleAppcSvcStrive, ruleAppcSvcAvoid12, ruleAppcSvcReject20,

		// ---- Design↔code alignment (align.go) ----
		ruleAlignMissingPkg, ruleAlignExtraPkg, ruleAlignLayerMismate,
		ruleAlignStalePlanned, ruleAlignExternalNonUtility, ruleAlignExternalUnwired,

		// ---- Code↔model conformance (rules_conformance.go) ----
		ruleCodeEdgeNotInModel, ruleModelEdgeNotInCode,

		// ---- System-test-plan (rules_testplan.go) ----
		ruleSTPOpExists, ruleSTPStaleContract, ruleSTPArgName, ruleSTPArgType,
		ruleSTPExpectShape, ruleSTPChainCover, ruleSTPWalkLegal, ruleSTPWalkParticipant,
		ruleSTPWalkMode, ruleSTPUCTrace, ruleSTPCaseKind,
	}
	set := make(map[RuleID]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}
