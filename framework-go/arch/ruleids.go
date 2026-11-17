package arch

import "sort"

// RuleIDs is every Rule string an arch violation can carry, sorted. It is the arch half of the
// answer to "does this rule have an emitter?": the doctrine registry (method-assets/doctrine)
// cites arch rules by id and its conformance test resolves each citation against this list.
//
// DISCIPLINE: a new violation rule is a named constant, listed here in the same change. The
// ruleids_test.go parse fails on a violation constructed with an inline string literal or with a
// constant this list omits.
func RuleIDs() []string {
	ids := []string{
		ruleHandActivityRegistration,
		ruleWorkflowInImplFile,
		ruleFileNotAllowed,
		ruleWorkflowFuncOutsideManager,
		ruleWorkflowFileMultipleFuncs,
		ruleWorkflowFileName,
		ruleScenarioTestsOnly,
		ruleTestPackageNotExternal,
		ruleTestCompose,
		ruleScenarioComposeTestOnly,
		ruleNoExportedHandIface,
		ruleNoForeignContractRedecl,
	}
	sort.Strings(ids)
	return ids
}
