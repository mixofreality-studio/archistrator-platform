package methodcheck

// AppCKind classifies an App-C item as either a directive (never violate)
// or a guideline (advise; waivable).
type AppCKind string

const (
	AppCDirective AppCKind = "directive"
	AppCGuideline AppCKind = "guideline"
)

// AppCClassification describes how the rule is enforced.
type AppCClassification string

const (
	AppCAutomatedCode     AppCClassification = "automated-code"
	AppCAutomatedDesign   AppCClassification = "automated-design"
	AppCAutomatedContract AppCClassification = "automated-contract"
	AppCHumanJudgment     AppCClassification = "human-judgment"
)

// AppCItem binds one Appendix-C item to its enforcement classification + rule.
type AppCItem struct {
	AppcRef        string // e.g. "SYS-3.6a", "PRIME", "SYS-D1"
	Kind           AppCKind
	Classification AppCClassification
	RuleID         RuleID // empty iff Classification == AppCHumanJudgment
}

// DefaultCoverage returns the complete Appendix-C coverage matrix.
// Every item in the book appears here bound to a rule or human-judgment.
// The matrix is assembled from per-domain slices to keep each section readable.
func DefaultCoverage() []AppCItem {
	var items []AppCItem
	items = append(items, primeAndDirectivesCoverage()...)
	items = append(items, systemDesignCoverage()...)
	items = append(items, projectDesignCoverage()...)
	items = append(items, projectTrackingCoverage()...)
	items = append(items, serviceContractCoverage()...)
	return items
}

// primeAndDirectivesCoverage is the Prime Directive plus the 9 directives.
func primeAndDirectivesCoverage() []AppCItem {
	return []AppCItem{
		// ---- Prime Directive ----
		{AppcRef: "PRIME", Kind: AppCDirective, Classification: AppCHumanJudgment},

		// ---- Directives (book §Directives, 9 items) ----
		{AppcRef: "DIR-1", Kind: AppCDirective, Classification: AppCHumanJudgment}, // Avoid functional decomposition
		{AppcRef: "DIR-2", Kind: AppCDirective, Classification: AppCHumanJudgment}, // Decompose based on volatility
		{AppcRef: "DIR-3", Kind: AppCDirective, Classification: AppCHumanJudgment}, // Provide composable design
		{AppcRef: "DIR-4", Kind: AppCDirective, Classification: AppCHumanJudgment}, // Features as aspects of integration
		{AppcRef: "DIR-5", Kind: AppCDirective, Classification: AppCHumanJudgment}, // Design iteratively, build incrementally
		{AppcRef: "DIR-6", Kind: AppCDirective, Classification: AppCHumanJudgment}, // Design project to build system
		{AppcRef: "DIR-7", Kind: AppCDirective, Classification: AppCHumanJudgment}, // Drive educated decisions with options
		{AppcRef: "DIR-8", Kind: AppCDirective, Classification: AppCHumanJudgment}, // Build along critical path
		{AppcRef: "DIR-9", Kind: AppCDirective, Classification: AppCHumanJudgment}, // Be on time throughout
	}
}

// systemDesignCoverage is the System Design Guidelines §1–§6.
func systemDesignCoverage() []AppCItem {
	return []AppCItem{
		// ---- System Design Guidelines §1 Requirements (5 items) ----
		{AppcRef: "SYS-1a", Kind: AppCGuideline, Classification: AppCHumanJudgment},                             // Capture behaviour not functionality
		{AppcRef: "SYS-1b", Kind: AppCGuideline, Classification: AppCHumanJudgment},                             // Describe with use cases
		{AppcRef: "SYS-1c", Kind: AppCGuideline, Classification: AppCAutomatedDesign, RuleID: ruleUcActDiagram}, // Activity diagrams for nested conditions
		{AppcRef: "SYS-1d", Kind: AppCGuideline, Classification: AppCHumanJudgment},                             // Eliminate solutions masquerading as requirements
		{AppcRef: "SYS-1e", Kind: AppCGuideline, Classification: AppCAutomatedDesign, RuleID: ruleArchChainCov}, // Validate design supports all core UCs

		// ---- §2 Cardinality (5 items) ----
		{AppcRef: "SYS-2a", Kind: AppCGuideline, Classification: AppCAutomatedDesign, RuleID: ruleSysCardMgr},
		{AppcRef: "SYS-2b", Kind: AppCGuideline, Classification: AppCHumanJudgment}, // Avoid more than handful of subsystems
		{AppcRef: "SYS-2c", Kind: AppCGuideline, Classification: AppCAutomatedDesign, RuleID: ruleAppcCardSubMgr},
		{AppcRef: "SYS-2d", Kind: AppCGuideline, Classification: AppCAutomatedDesign, RuleID: ruleSysCardRatio},
		{AppcRef: "SYS-2e", Kind: AppCGuideline, Classification: AppCHumanJudgment}, // RA may access >1 resource

		// ---- §3 Attributes (6 items) ----
		{AppcRef: "SYS-3a", Kind: AppCGuideline, Classification: AppCHumanJudgment}, // Volatility decreases top-down
		{AppcRef: "SYS-3b", Kind: AppCGuideline, Classification: AppCHumanJudgment}, // Reuse increases top-down
		{AppcRef: "SYS-3c", Kind: AppCGuideline, Classification: AppCHumanJudgment}, // Don't encapsulate nature of business
		{AppcRef: "SYS-3d", Kind: AppCGuideline, Classification: AppCHumanJudgment}, // Managers almost expendable
		{AppcRef: "SYS-3e", Kind: AppCGuideline, Classification: AppCHumanJudgment}, // Design symmetric
		{AppcRef: "SYS-3f", Kind: AppCGuideline, Classification: AppCHumanJudgment}, // No public channels for internal interactions

		// ---- §4 Layers (5 items) ----
		{AppcRef: "SYS-4a", Kind: AppCGuideline, Classification: AppCAutomatedDesign, RuleID: ruleAppcArchOpen},
		{AppcRef: "SYS-4b", Kind: AppCGuideline, Classification: AppCAutomatedDesign, RuleID: ruleAppcArchSemiOpen},
		{AppcRef: "SYS-4c-i", Kind: AppCGuideline, Classification: AppCAutomatedDesign, RuleID: ruleSysNoUp},
		{AppcRef: "SYS-4c-ii", Kind: AppCGuideline, Classification: AppCAutomatedDesign, RuleID: ruleSysNoSide},
		{AppcRef: "SYS-4c-iii", Kind: AppCGuideline, Classification: AppCAutomatedDesign, RuleID: ruleSysNoSkip},
		{AppcRef: "SYS-4c-iv", Kind: AppCGuideline, Classification: AppCHumanJudgment}, // Resolve via queued/async
		{AppcRef: "SYS-4d", Kind: AppCGuideline, Classification: AppCHumanJudgment},    // Extend via subsystems

		// ---- §5 Interaction rules (4 items) ----
		{AppcRef: "SYS-5a", Kind: AppCGuideline, Classification: AppCAutomatedDesign, RuleID: ruleAppcIntUtility},
		{AppcRef: "SYS-5b", Kind: AppCGuideline, Classification: AppCAutomatedDesign, RuleID: ruleAppcIntMgrEngRA},
		{AppcRef: "SYS-5c", Kind: AppCGuideline, Classification: AppCAutomatedDesign, RuleID: ruleAppcIntMgrEng},
		{AppcRef: "SYS-5d", Kind: AppCGuideline, Classification: AppCAutomatedDesign, RuleID: ruleSysNoSide},

		// ---- §6 Interaction don'ts (9 items — directives) ----
		{AppcRef: "SYS-6a", Kind: AppCDirective, Classification: AppCAutomatedDesign, RuleID: ruleAppcDontClientMultiMgr},
		{AppcRef: "SYS-6b", Kind: AppCDirective, Classification: AppCAutomatedDesign, RuleID: ruleAppcDontMgrMultiQueue},
		{AppcRef: "SYS-6c", Kind: AppCDirective, Classification: AppCAutomatedDesign, RuleID: ruleAppcDontEngineQueue},
		{AppcRef: "SYS-6d", Kind: AppCDirective, Classification: AppCAutomatedDesign, RuleID: ruleAppcDontRAQueue},
		{AppcRef: "SYS-6e", Kind: AppCDirective, Classification: AppCAutomatedDesign, RuleID: ruleAppcDontClientPub},
		{AppcRef: "SYS-6f", Kind: AppCDirective, Classification: AppCAutomatedDesign, RuleID: ruleAppcDontEnginePub},
		{AppcRef: "SYS-6g", Kind: AppCDirective, Classification: AppCAutomatedDesign, RuleID: ruleAppcDontRAPub},
		{AppcRef: "SYS-6h", Kind: AppCDirective, Classification: AppCAutomatedDesign, RuleID: ruleAppcDontResourcePub},
		{AppcRef: "SYS-6i", Kind: AppCDirective, Classification: AppCAutomatedDesign, RuleID: ruleAppcDontNonMgrSub},
	}
}

// projectDesignCoverage is the Project Design Guidelines §1–§7 (all human-judgment).
func projectDesignCoverage() []AppCItem {
	return []AppCItem{
		// ---- Project Design Guidelines §1 General (7 items) ----
		{AppcRef: "PROJ-1a", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-1b", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-1c", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-1d", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-1e", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-1f", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-1g", Kind: AppCGuideline, Classification: AppCHumanJudgment},

		// ---- §2 Staffing (8 items) ----
		{AppcRef: "PROJ-2a", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-2b", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-2c", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-2d", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-2e", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-2f", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-2g", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-2h", Kind: AppCGuideline, Classification: AppCHumanJudgment},

		// ---- §3 Integration (2 items) ----
		{AppcRef: "PROJ-3a", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-3b", Kind: AppCGuideline, Classification: AppCHumanJudgment},

		// ---- §4 Estimations (7 items) ----
		{AppcRef: "PROJ-4a", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-4b", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-4c", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-4d", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-4e", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-4f", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-4g", Kind: AppCGuideline, Classification: AppCHumanJudgment},

		// ---- §5 Project network (10 items) ----
		{AppcRef: "PROJ-5a", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-5b", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-5c", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-5d", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-5e", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-5f", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-5g", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-5h", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-5i", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-5j", Kind: AppCGuideline, Classification: AppCHumanJudgment},

		// ---- §6 Time and cost (7 items) ----
		{AppcRef: "PROJ-6a", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-6b", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-6c", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-6d", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-6e", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-6f", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-6g", Kind: AppCGuideline, Classification: AppCHumanJudgment},

		// ---- §7 Risk (9 items) ----
		{AppcRef: "PROJ-7a", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-7b", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-7c-i", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-7c-ii", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-7d", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-7e", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-7f", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-7g", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "PROJ-7h", Kind: AppCGuideline, Classification: AppCHumanJudgment},
	}
}

// projectTrackingCoverage is the Project Tracking Guidelines (all human-judgment).
func projectTrackingCoverage() []AppCItem {
	return []AppCItem{
		// ---- Project Tracking Guidelines (6 items) ----
		{AppcRef: "TRACK-1", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "TRACK-2", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "TRACK-3", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "TRACK-4", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "TRACK-5", Kind: AppCGuideline, Classification: AppCHumanJudgment},
		{AppcRef: "TRACK-6", Kind: AppCGuideline, Classification: AppCHumanJudgment},
	}
}

// serviceContractCoverage is the §6 Service Contract Design Guidelines.
func serviceContractCoverage() []AppCItem {
	return []AppCItem{
		// ---- §6 Service Contract Design Guidelines (9 items) ----
		{AppcRef: "SVC-1", Kind: AppCGuideline, Classification: AppCHumanJudgment}, // Design reusable contracts
		{AppcRef: "SVC-2a", Kind: AppCGuideline, Classification: AppCAutomatedContract, RuleID: ruleAppcSvcSingle},
		{AppcRef: "SVC-2b", Kind: AppCGuideline, Classification: AppCAutomatedContract, RuleID: ruleAppcSvcStrive},
		{AppcRef: "SVC-2c", Kind: AppCGuideline, Classification: AppCAutomatedContract, RuleID: ruleAppcSvcAvoid12},
		{AppcRef: "SVC-2d", Kind: AppCDirective, Classification: AppCAutomatedContract, RuleID: ruleAppcSvcReject20},
		{AppcRef: "SVC-3", Kind: AppCGuideline, Classification: AppCHumanJudgment}, // Avoid property-like operations
		{AppcRef: "SVC-4", Kind: AppCGuideline, Classification: AppCHumanJudgment}, // Limit contracts per service
		{AppcRef: "SVC-5", Kind: AppCGuideline, Classification: AppCHumanJudgment}, // Avoid junior hand-offs
		{AppcRef: "SVC-6", Kind: AppCGuideline, Classification: AppCHumanJudgment}, // Architect/senior designs contracts
	}
}
