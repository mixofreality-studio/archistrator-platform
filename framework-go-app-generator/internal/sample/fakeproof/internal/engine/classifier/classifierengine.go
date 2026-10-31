package classifier

import fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"

// newClassifierEngine is the hand-written builder the generated NewClassifierEngine
// delegates to. The compile-proof sample only needs it to exist and to satisfy the
// generated interface.
func newClassifierEngine() ClassifierEngine { return classifierEngine{} }

type classifierEngine struct{}

func (classifierEngine) Classify(_ fweng.Context, _ WidgetID) (Classification, error) {
	var zero Classification
	return zero, nil
}
