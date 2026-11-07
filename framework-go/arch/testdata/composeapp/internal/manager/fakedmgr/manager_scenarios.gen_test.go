package fakedmgr_test

import (
	"testing"

	"example.com/composeapp/internal/manager/fakedmgr"
)

// inFileFake hand-writes a subject: its method set matches the contract
// interface, so the scenario would test the fake, not the component.
type inFileFake struct{}

func (*inFileFake) Do() error { return nil }

func TestScenario_Do(t *testing.T) {
	var subject fakedmgr.FakedManager = &inFileFake{}
	if err := subject.Do(); err != nil {
		t.Fatal(err)
	}
}
