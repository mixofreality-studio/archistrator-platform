package projectmodel

import (
	"slices"
)

// Contract order. Construction records the service contracts one component at a time, and the
// construction record verb refuses a project Load refuses, so Load judges every prefix of the
// contract set, in whatever order construction records it. A reference by contract key (a
// contract's component dep, a deployment binding's component or provides) is resolved against
// the contracts recorded so far, and it resolves to exactly one of three things:
//
//   - resolvedRef: the contract is recorded, and the reference is judged in full against it;
//   - pendingRef: the System declares the component but its contract is not recorded yet, and
//     some contract-bearing component still has none. Not a problem: the order the contracts
//     happen to be recorded in must not decide whether a valid design loads;
//   - danglingRef: the System does not declare the component (or there is no System), or every
//     contract-bearing component already has its contract, so this one never will. A problem.

// reference is what a contract-key reference resolves to.
//
//sumtype:decl
type reference interface{ isReference() }

type resolvedRef struct{ contract *Contract }

type pendingRef struct{ component *SystemComponent }

type danglingRef struct{}

func (resolvedRef) isReference() {}
func (pendingRef) isReference()  {}
func (danglingRef) isReference() {}

// resolver resolves contract-key references against the recorded contracts and the System.
type resolver struct {
	contracts map[string]*Contract
	system    *System
	// incomplete: a built, contract-bearing System component has no recorded contract yet.
	incomplete bool
}

func newResolver(contracts map[string]*Contract, system *System) *resolver {
	return &resolver{contracts: contracts, system: system, incomplete: contractsIncomplete(contracts, system)}
}

// resolve is what key resolves to.
func (r *resolver) resolve(key string) reference {
	if c, ok := r.contracts[key]; ok {
		return resolvedRef{contract: c}
	}
	if r.system == nil || !r.incomplete {
		return danglingRef{}
	}
	if sc, ok := r.system.ComponentByContractKey(key); ok {
		return pendingRef{component: sc}
	}
	return danglingRef{}
}

// dangling reports whether ref can never resolve: the only one of the three that is a problem
// where nothing about the target's contract is read.
func dangling(ref reference) bool {
	switch ref.(type) {
	case danglingRef:
		return true
	case pendingRef, resolvedRef:
		return false
	}
	return false
}

// contractsIncomplete reports whether the System has a built, contract-bearing component that no
// recorded contract joins. Without a System nothing is owed.
func contractsIncomplete(contracts map[string]*Contract, system *System) bool {
	if system == nil {
		return false
	}
	have := map[string]bool{}
	for key := range contracts {
		if c, ok := system.ComponentByContractKey(key); ok {
			have[c.ID] = true
		}
	}
	return slices.ContainsFunc(system.Components, func(c SystemComponent) bool {
		return c.OwesAuthoredContract() && !have[c.ID]
	})
}
