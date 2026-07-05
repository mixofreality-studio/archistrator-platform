package methodcheck

import (
	"strings"
	"testing"
)

// rules_system_test.go — coverage for SYSTEM-LAYER-DEGENERATE (F81): the whole-system
// structure signals (zero Managers / zero ResourceAccess) and the per-component
// name↔layer contradiction signal.

func TestSystemLayerDegenerate_HealthySystemClean(t *testing.T) {
	s := System{Components: []Component{
		comp(t, "WebClient", kindClient),
		comp(t, "OrderManager", kindManager),
		comp(t, "OrderAccess", kindResourceAccess),
	}}
	if f := systemLayerDegenerate(s); len(f) != 0 {
		t.Fatalf("healthy system should be clean, got: %+v", f)
	}
}

func TestSystemLayerDegenerate_AllClientFlagged(t *testing.T) {
	// The live F81 corruption: every component's layer omitted → defaulted to client,
	// re-serialized as layer=client for all. Kinds happen to be correct wire strings.
	s := System{Components: []Component{
		{ID: "m", Name: "OrderManager", Kind: kindClient, Layer: layerClient},
		{ID: "e", Name: "PricingEngine", Kind: kindClient, Layer: layerClient},
		{ID: "ra", Name: "OrderAccess", Kind: kindClient, Layer: layerClient},
	}}
	f := systemLayerDegenerate(s)
	var zeroMgr, zeroRA, nameMismatch int
	for _, fi := range f {
		if fi.RuleID != ruleSystemLayerDegenerate {
			t.Fatalf("unexpected rule id %q", fi.RuleID)
		}
		switch {
		case strings.Contains(fi.Message, "zero Managers"):
			zeroMgr++
		case strings.Contains(fi.Message, "zero ResourceAccess"):
			zeroRA++
		case strings.Contains(fi.Message, "name ends in"):
			nameMismatch++
		}
	}
	if zeroMgr != 1 || zeroRA != 1 {
		t.Fatalf("expected one zero-managers and one zero-resourceAccess finding, got mgr=%d ra=%d (%+v)", zeroMgr, zeroRA, f)
	}
	if nameMismatch != 3 {
		t.Fatalf("expected 3 name/layer mismatch findings, got %d (%+v)", nameMismatch, f)
	}
}

func TestSystemLayerDegenerate_NameLayerMismatch(t *testing.T) {
	s := System{Components: []Component{
		comp(t, "OrderManager", kindManager),
		comp(t, "OrderAccess", kindResourceAccess),
		// "…Engine" name sitting in the client layer.
		{ID: "e", Name: "PricingEngine", Kind: kindEngine, Layer: layerClient},
	}}
	f := systemLayerDegenerate(s)
	if len(f) != 1 {
		t.Fatalf("expected exactly one finding, got: %+v", f)
	}
	if !strings.Contains(f[0].Message, "PricingEngine") || !strings.Contains(f[0].Message, layerEngine) {
		t.Fatalf("finding should name the offending component and its expected layer, got: %v", f[0].Message)
	}
}

func TestNameLayerMismatch_Cases(t *testing.T) {
	cases := []struct {
		name     string
		layer    string
		want     string
		suffix   string
		mismatch bool
	}{
		{"OrderManager", layerManager, layerManager, "Manager", false},
		{"OrderManager", layerClient, layerManager, "Manager", true},
		{"EventStore", layerResource, layerResource, "Store", false},
		{"EventStore", layerClient, layerResource, "Store", true},
		{"AuditResource", layerResource, layerResource, "Resource", false},
		{"Utilities", layerUtility, layerUtility, "", false}, // no stereotype suffix
		{"OrderManager", "", layerManager, "Manager", false}, // empty layer never mismatches here
	}
	for _, c := range cases {
		want, suffix, mismatch := nameLayerMismatch(c.name, c.layer)
		if mismatch != c.mismatch || suffix != c.suffix || (mismatch && want != c.want) {
			t.Fatalf("nameLayerMismatch(%q,%q) = (%q,%q,%v), want (%q,%q,%v)",
				c.name, c.layer, want, suffix, mismatch, c.want, c.suffix, c.mismatch)
		}
	}
}


