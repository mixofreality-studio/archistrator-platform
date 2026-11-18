package conformance

import (
	"encoding/json"
	"maps"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// Contract order (Tier 0). Construction records the service contracts one component at a time,
// and the record verb refuses any project projectmodel.Load refuses, so Load judges every prefix
// of the contract set in whatever order construction happens to record it. Before the fix a valid
// design was refused depending on that order: greenfield in key order met PM-DEPLOY-REF until
// orderStateAccess was recorded, then PM-DEP-TARGET on pricingEngine.

// maxExhaustiveContracts is the largest contract set whose every recording order is tried; a
// larger one is tried in sampledOrders orders.
const (
	maxExhaustiveContracts = 6
	sampledOrders          = 20
)

// contractOrders is the recording orders tried for keys: every permutation when there are at most
// maxExhaustiveContracts keys, else sampledOrders fixed orders that start with key order and its
// reverse and continue with seeded shuffles.
func contractOrders(keys []string) [][]string {
	sorted := slices.Sorted(slices.Values(keys))
	if len(sorted) <= maxExhaustiveContracts {
		return permutations(sorted)
	}
	reverse := slices.Clone(sorted)
	slices.Reverse(reverse)
	orders := [][]string{sorted, reverse}
	rng := rand.New(rand.NewPCG(2026, 1118))
	for len(orders) < sampledOrders {
		o := slices.Clone(sorted)
		rng.Shuffle(len(o), func(i, j int) { o[i], o[j] = o[j], o[i] })
		orders = append(orders, o)
	}
	return orders
}

// permutations is every ordering of keys.
func permutations(keys []string) [][]string {
	if len(keys) <= 1 {
		return [][]string{slices.Clone(keys)}
	}
	var out [][]string
	for i, k := range keys {
		rest := append(slices.Clone(keys[:i]), keys[i+1:]...)
		for _, p := range permutations(rest) {
			out = append(out, append([]string{k}, p...))
		}
	}
	return out
}

// Every prefix of every tried recording order of each fixture's contracts loads under Tier 0, with
// every design slot recorded.
func TestTier0_EveryContractRecordingOrderLoads(t *testing.T) {
	for _, name := range FixtureNames() {
		t.Run(name, func(t *testing.T) {
			raw, err := Fixture(name)
			if err != nil {
				t.Fatal(err)
			}
			var doc map[string]json.RawMessage
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatal(err)
			}
			var contracts map[string]json.RawMessage
			if err := json.Unmarshal(doc["serviceContracts"], &contracts); err != nil {
				t.Fatal(err)
			}
			if len(contracts) < 2 {
				t.Fatalf("%d contract(s): no recording order to vary", len(contracts))
			}
			keys := slices.Collect(maps.Keys(contracts))
			orders := contractOrders(keys)
			want := sampledOrders
			if len(keys) <= maxExhaustiveContracts {
				want = factorial(len(keys))
			}
			if len(orders) != want {
				t.Fatalf("%d orders tried, want %d", len(orders), want)
			}
			for _, order := range orders {
				recorded := map[string]json.RawMessage{}
				for i, key := range order {
					recorded[key] = contracts[key]
					b, err := json.Marshal(recorded)
					if err != nil {
						t.Fatal(err)
					}
					doc["serviceContracts"] = b
					prefix, err := json.Marshal(doc)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := projectmodel.Load(prefix); err != nil {
						t.Errorf("order %s, after recording %d of %d: Load refuses:\n%v", strings.Join(order, ","), i+1, len(order), err)
						break
					}
				}
			}
		})
	}
}

func factorial(n int) int {
	f := 1
	for i := 2; i <= n; i++ {
		f *= i
	}
	return f
}
