package arch_test

import (
	"testing"

	"github.com/davidmarne/archistrator-platform/framework-go/arch"
)

// framework-go has no internal/ tree; its layer packages live at the module root.
// This self-test exercises the checker's layer-dependency and Temporal-isolation
// rules against framework-go itself: engine/ and resourceaccess/ must be
// Temporal-free, manager/ may import Temporal. Naming/return rules are disabled
// (nil suffix) because these packages expose Error types, not {Name}Access ports.
func TestFrameworkGoObeysItsOwnLayerRules(t *testing.T) {
	spec := arch.Spec{
		ModuleRoot:   "..",
		ModulePrefix: "github.com/davidmarne/archistrator-platform/framework-go/",
		Patterns:     []string{"./manager/...", "./engine/...", "./resourceaccess/..."},
		Layers: []arch.Layer{
			{Name: "Manager", DirPrefix: "manager"},
			{Name: "Engine", DirPrefix: "engine"},
			{Name: "ResourceAccess", DirPrefix: "resourceaccess"},
		},
		TemporalLayer: "Manager",
	}
	arch.Check(t, spec)
}
