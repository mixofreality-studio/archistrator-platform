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

	// ValidateVolatilities(volatilities, glossary, scrubbedRequirements).
	if v, ok, err := p.volatilities(); err != nil {
		return nil, err
	} else if ok {
		g, gok, gerr := p.glossary()
		if gerr != nil {
			return nil, gerr
		}
		sr, srok, srerr := p.scrubbedRequirements()
		if srerr != nil {
			return nil, srerr
		}
		if gok && srok {
			res, verr := validateVolatilities(v, g, sr)
			if verr != nil {
				return nil, fmt.Errorf("ValidateVolatilities: %w", verr)
			}
			all = append(all, res.Findings...)
		}
	}

	// ValidateCoreUseCases(coreUseCases).
	if c, ok, err := p.coreUseCases(); err != nil {
		return nil, err
	} else if ok {
		res, verr := validateCoreUseCases(c)
		if verr != nil {
			return nil, fmt.Errorf("ValidateCoreUseCases: %w", verr)
		}
		all = append(all, res.Findings...)
	}

	// ValidateArchitecture(system, coreUseCases).
	if s, ok, err := p.system(); err != nil {
		return nil, err
	} else if ok {
		c, cok, cerr := p.coreUseCases()
		if cerr != nil {
			return nil, cerr
		}
		if cok {
			res, verr := validateArchitecture(s, c)
			if verr != nil {
				return nil, fmt.Errorf("ValidateArchitecture: %w", verr)
			}
			all = append(all, res.Findings...)
		}
	}

	// ValidateOperationalConcepts(operationalConcepts, mission, system).
	if o, ok, err := p.operationalConcepts(); err != nil {
		return nil, err
	} else if ok {
		m, mok, merr := p.mission()
		if merr != nil {
			return nil, merr
		}
		s, sok, serr := p.system()
		if serr != nil {
			return nil, serr
		}
		if mok && sok {
			res, verr := validateOperationalConcepts(o, m, s)
			if verr != nil {
				return nil, fmt.Errorf("ValidateOperationalConcepts: %w", verr)
			}
			all = append(all, res.Findings...)
		}
	}

	// ValidateStandardCheck(standardCheck).
	if sc, ok, err := p.standardCheck(); err != nil {
		return nil, err
	} else if ok {
		res, verr := validateStandardCheck(sc)
		if verr != nil {
			return nil, fmt.Errorf("ValidateStandardCheck: %w", verr)
		}
		all = append(all, res.Findings...)
	}

	return all, nil
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
