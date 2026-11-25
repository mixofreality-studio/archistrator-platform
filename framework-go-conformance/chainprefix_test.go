//go:build conformance

package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestConformance_ChainOverContractPrefixes runs the Tier 2 chain (RunChainOn, the admission:tier2
// check every construction commit must pass) over the PARTIAL designs a project goes through while
// its contracts are recorded: no contract at N-ATP, then one more per activity in network order.
// TestConformance_Chain only chains complete fixtures, so ISS-027-1 and ISS-027-2 (composegen
// refused an mcpApp Client fronting a Manager with no contract yet) shipped green and each made
// every early construction activity unpassable. Every prefix must build.
//
// The prefixes are those of todomvc's non-Client contracts, callee before caller along the System's
// relationships (ResourceAccess/Utility, then Engine, then Manager, ties by key), computed from
// the fixture. A machine Client's (rest, mcpTools) contract enters with its Manager; browser-surface
// (web, mcpApp) contracts and phaseArtifacts are held out of every prefix, as later activities
// record them.
func TestConformance_ChainOverContractPrefixes(t *testing.T) {
	raw, err := Fixture("todomvc")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	slots, _ := doc["slots"].(map[string]any)
	contracts, _ := doc["serviceContracts"].(map[string]any)
	sys := systemOf(t, slots)

	// Non-vacuity: the fixture has the shapes the prefixes exist to cover.
	surfaces := map[string]bool{}
	managerKey := ""
	layerOf := map[string]int{}    // component id -> network rank
	keyOfID := map[string]string{} // component id -> contract key
	for _, c := range sys.Components {
		keyOfID[c.ID] = contractKeyOf(c.ID, c.ContractKey)
		switch strings.ToLower(c.Kind) {
		case "client":
			surfaces[c.Surface] = true
		case "manager":
			managerKey = keyOfID[c.ID]
			layerOf[c.ID] = 2
		case "engine":
			layerOf[c.ID] = 1
		default:
			layerOf[c.ID] = 0
		}
	}
	if !surfaces["mcpApp"] || managerKey == "" {
		t.Fatalf("fixture has client surfaces %v and manager %q: it must hold an mcpApp Client and a Manager", surfaces, managerKey)
	}

	// Network order over the contracted non-Client components: Kahn, ready ties by layer then key.
	pending := map[string]string{} // component id -> key
	for _, c := range sys.Components {
		if strings.EqualFold(c.Kind, "client") {
			continue
		}
		if _, ok := contracts[keyOfID[c.ID]]; ok {
			pending[c.ID] = keyOfID[c.ID]
		}
	}
	var order []string
	for len(pending) > 0 {
		var ready []string // component ids
		for id := range pending {
			blocked := false
			for _, r := range sys.Relationships {
				if _, wait := pending[r.To]; r.From == id && wait && r.To != id {
					blocked = true
					break
				}
			}
			if !blocked {
				ready = append(ready, id)
			}
		}
		if len(ready) == 0 {
			t.Fatalf("the System's relationships among %v form a cycle: no network order", slices.Sorted(maps.Values(pending)))
		}
		next := slices.MinFunc(ready, func(a, b string) int {
			if layerOf[a] != layerOf[b] {
				return layerOf[a] - layerOf[b]
			}
			return strings.Compare(pending[a], pending[b])
		})
		order = append(order, pending[next])
		delete(pending, next)
	}
	co := contractOrder{name: "network order", order: func([]string) []string { return order }}
	recorded, withManager, _ := contractSchedule(t, sys, slices.Collect(maps.Keys(contracts)), co)

	managerAt := slices.Index(recorded, managerKey)
	if managerAt < 1 {
		t.Fatalf("network order %v: the Manager %s must follow some non-Manager contract (the ISS-027-2 shape)", recorded, managerKey)
	}

	// The envelope: every slot, no contracts, no phaseArtifacts.
	prefix := map[string]any{}
	for k, v := range doc {
		switch k {
		case "serviceContracts", "phaseArtifacts":
		default:
			prefix[k] = v
		}
	}
	if _, has := prefix["serviceContracts"]; has {
		t.Fatal("prefix 0 must have no serviceContracts key")
	}

	runPrefix := func(t *testing.T) {
		b, err := json.Marshal(prefix)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
		defer cancel()
		start := time.Now()
		rep, err := RunChainOn(ctx, "todomvc", b, t.TempDir())
		for _, s := range rep.Steps {
			t.Logf("%-28s exit=%d %s", s.Name, s.ExitCode, s.Duration.Round(time.Millisecond))
		}
		t.Logf("prefix total %s", time.Since(start).Round(time.Millisecond))
		if err != nil {
			t.Errorf("RunChainOn: %v", err)
		}
		if len(rep.Steps) == 0 {
			t.Fatal("chain ran no steps")
		}
		for _, s := range rep.Steps {
			if s.ExitCode != 0 {
				t.Errorf("step %s (%v) exit %d:\n%s", s.Name, s.Argv, s.ExitCode, s.Stderr)
			}
		}
	}

	t.Run("prefix-0-no-contracts", runPrefix)
	for i, key := range recorded {
		addContracts(prefix, contracts, append([]string{key}, withManager[key]...)...)
		t.Run(fmt.Sprintf("prefix-%d-%s", i+1, key), runPrefix)
	}
}
