package scenario

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"unicode"
)

// Normalize folds a component id to the comparison key shared by contract
// keys (camelCase) and slot-5 ids (kebab-case): lower-case, non-alphanumerics removed.
func Normalize(id string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(id) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// CanonicalJSON encodes scenarios deterministically (sorted by ID, no HTML escaping, "\n"-terminated).
func CanonicalJSON(s []Scenario) ([]byte, error) {
	cp := make([]Scenario, len(s))
	copy(cp, s)
	sort.SliceStable(cp, func(i, j int) bool { return cp[i].ID < cp[j].ID })
	for i := range cp {
		cp[i] = withNonNilSlices(cp[i])
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cp); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Fingerprint is sha256 of CanonicalJSON, hex.
func Fingerprint(s []Scenario) (string, error) {
	b, err := CanonicalJSON(s)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// withNonNilSlices makes nil and empty slices encode identically ("[]").
// It never writes through to the caller's scenario: the stimuli slice is
// copied before its outcomes are normalized.
func withNonNilSlices(s Scenario) Scenario {
	if s.Path == nil {
		s.Path = []string{}
	}
	if s.Guards == nil {
		s.Guards = []Guard{}
	}
	s.Preconditions = nonNilOutcome(s.Preconditions)
	stimuli := make([]Stimulus, len(s.Stimuli))
	copy(stimuli, s.Stimuli)
	for i := range stimuli {
		stimuli[i].Outcome = nonNilOutcome(stimuli[i].Outcome)
	}
	s.Stimuli = stimuli
	return s
}

func nonNilOutcome(o Outcome) Outcome {
	if o.ExpectedOutputs == nil {
		o.ExpectedOutputs = []Output{}
	}
	if o.ExpectedStates == nil {
		o.ExpectedStates = []State{}
	}
	if o.Hints == nil {
		o.Hints = []string{}
	}
	return o
}
