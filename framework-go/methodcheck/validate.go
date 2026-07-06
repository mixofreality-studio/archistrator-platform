package methodcheck

import "fmt"

// validate.go is the design-rule orchestration: it mirrors the aiarch
// cmd/aiarch-validate `validateProject` — each Method-invariant verb runs ONLY
// when its prerequisite committed slots are present, and a verb's ContractMisuse
// (a dependent artifact committed without its prerequisite) surfaces as an error
// the caller treats as a coherence failure. The findings from all run verbs merge.

// ValidateProject runs every applicable Method-invariant verb over the committed
// typed slots of p. A verb runs ONLY when its committed prerequisite models are
// present. A non-nil error is a coherence fault (a dependent artifact committed
// without its prerequisite), exactly as the CLI's validateProject returns.
func ValidateProject(p Project) ([]Finding, error) {
	var all []Finding
	for _, run := range []func(Project) ([]Finding, error){
		glossaryFindings,
		scrubbedRequirementsFindings,
		volatilitiesFindings,
		coreUseCasesFindings,
		architectureFindings,
		operationalConceptsFindings,
		standardCheckFindings,
		appCFindings,
		systemTestPlanFindings,
	} {
		f, err := run(p)
		if err != nil {
			return nil, err
		}
		all = append(all, f...)
	}
	return all, nil
}

// glossaryFindings runs the GLOSS-FOURQ twin. It fires only when the Glossary slot is
// committed; a pre-glossary project is a no-op.
func glossaryFindings(p Project) ([]Finding, error) {
	g, ok, err := p.glossary()
	if err != nil || !ok {
		return nil, err
	}
	res, verr := validateGlossary(g)
	if verr != nil {
		return nil, fmt.Errorf("ValidateGlossary: %w", verr)
	}
	return res.Findings, nil
}

// scrubbedRequirementsFindings runs the SR-ID-UNIQUE twin. It fires only when the
// ScrubbedRequirements slot is committed; a pre-scrub project is a no-op.
func scrubbedRequirementsFindings(p Project) ([]Finding, error) {
	sr, ok, err := p.scrubbedRequirements()
	if err != nil || !ok {
		return nil, err
	}
	res, verr := validateScrubbedRequirements(sr)
	if verr != nil {
		return nil, fmt.Errorf("ValidateScrubbedRequirements: %w", verr)
	}
	return res.Findings, nil
}

func volatilitiesFindings(p Project) ([]Finding, error) {
	v, ok, err := p.volatilities()
	if err != nil || !ok {
		return nil, err
	}
	g, gok, gerr := p.glossary()
	if gerr != nil {
		return nil, gerr
	}
	sr, srok, srerr := p.scrubbedRequirements()
	if srerr != nil {
		return nil, srerr
	}
	if !gok || !srok {
		return nil, nil
	}
	res, verr := validateVolatilities(v, g, sr)
	if verr != nil {
		return nil, fmt.Errorf("ValidateVolatilities: %w", verr)
	}
	return res.Findings, nil
}

func coreUseCasesFindings(p Project) ([]Finding, error) {
	c, ok, err := p.coreUseCases()
	if err != nil || !ok {
		return nil, err
	}
	res, verr := validateCoreUseCases(c)
	if verr != nil {
		return nil, fmt.Errorf("ValidateCoreUseCases: %w", verr)
	}
	return res.Findings, nil
}

func architectureFindings(p Project) ([]Finding, error) {
	s, ok, err := p.system()
	if err != nil || !ok {
		return nil, err
	}
	c, cok, cerr := p.coreUseCases()
	if cerr != nil {
		return nil, cerr
	}
	if !cok {
		return nil, nil
	}
	res, verr := validateArchitecture(s, c)
	if verr != nil {
		return nil, fmt.Errorf("ValidateArchitecture: %w", verr)
	}
	return res.Findings, nil
}

func operationalConceptsFindings(p Project) ([]Finding, error) {
	o, ok, err := p.operationalConcepts()
	if err != nil || !ok {
		return nil, err
	}
	m, mok, merr := p.mission()
	if merr != nil {
		return nil, merr
	}
	s, sok, serr := p.system()
	if serr != nil {
		return nil, serr
	}
	if !mok || !sok {
		return nil, nil
	}
	res, verr := validateOperationalConcepts(o, m, s)
	if verr != nil {
		return nil, fmt.Errorf("ValidateOperationalConcepts: %w", verr)
	}
	return res.Findings, nil
}

func standardCheckFindings(p Project) ([]Finding, error) {
	sc, ok, err := p.standardCheck()
	if err != nil || !ok {
		return nil, err
	}
	res, verr := validateStandardCheck(sc)
	if verr != nil {
		return nil, fmt.Errorf("ValidateStandardCheck: %w", verr)
	}
	return res.Findings, nil
}

func appCFindings(p Project) ([]Finding, error) {
	s, sOK, sErr := p.system()
	if sErr != nil || !sOK {
		return nil, sErr
	}
	sc, scOK, scErr := p.standardCheck()
	if scErr != nil {
		return nil, scErr
	}
	var findings []Finding
	findings = append(findings, appCInteractionDonts(s)...)
	findings = append(findings, appCClosedArch(s)...)
	findings = append(findings, appCCardinality(s)...)
	findings = append(findings, appCServiceContract(s)...)
	if scOK {
		findings = applyWaivers(findings, sc)
	}
	return findings, nil
}

// systemTestPlanFindings runs the STP-* System-Test-Plan family (rules_testplan.go)
// as a verb-gated design rule, following the same prerequisite-gating posture as the
// design verbs above: it fires ONLY when .testingState.systemTestPlan is non-empty and
// is a total no-op otherwise (nil, nil), so pre-testing-phase projects are unaffected.
// When the plan IS committed but a prerequisite (service contracts, the System in slot
// 5, the core use cases in slot 4) is not, validateSystemTestPlan returns a
// *ContractMisuseError — the same coherence fault the caller (Check) surfaces via
// t.Errorf. ValidateProject (and thus ValidateProjectJSON) carries no ProjectSpec, so
// the STP walk case-folds through defaultNormalizer; step→contract-op resolution stays
// case-sensitive, so a plan step must name a frozen contract operation exactly.
func systemTestPlanFindings(p Project) ([]Finding, error) {
	return validateSystemTestPlan(p, nil)
}

// ValidateProjectJSON is the pure, non-test seam: decode a raw
// `.aiarch/state/project.json` and run ValidateProject. Empty/absent bytes decode
// to no project and return no findings (nothing to validate), matching the CLI's
// clean-pass-on-empty.
func ValidateProjectJSON(raw []byte) ([]Finding, error) {
	p, ok, err := DecodeProject(raw)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return ValidateProject(p)
}
