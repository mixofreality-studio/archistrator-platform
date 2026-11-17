package appgen

import (
	"os"
	"reflect"
	"testing"
)

// AdmitTier1 shares what its consumers would each recompute from the project (the loaded model,
// the decoded envelope, the `appgen clients` emission). Sharing is only sound while no consumer
// writes to what it is handed, so every consumer must emit the same files, or fail the same way,
// with the memo as without it, in the order a pass runs them.
func TestEmitMemo_ConsumersEmitWhatTheyEmitWithoutIt(t *testing.T) {
	own, err := os.ReadFile("../testdata/archistrator.project.json")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]Input{
		"todomvc":      fixtureInput(t, "todomvc", "server", false),
		"archistrator": {Project: own, ModulePath: "github.com/mixofreality-studio/archistrator/server", ModuleDir: "server", Config: DefaultConfig()},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			shared := in
			shared.memo = &emitMemo{}
			for _, c := range AllConsumers() {
				want, wantErr := c.Emit(in)
				got, gotErr := c.Emit(shared)
				if (wantErr == nil) != (gotErr == nil) || (wantErr != nil && wantErr.Error() != gotErr.Error()) {
					t.Fatalf("%s: error %v with the memo, %v without", c.Name, gotErr, wantErr)
				}
				if !reflect.DeepEqual(want, got) {
					t.Fatalf("%s: the emission differs with the memo (%d files, %d without)", c.Name, len(got), len(want))
				}
			}
			if !shared.memo.modelDone || !shared.memo.decodedDone || !shared.memo.clientsReady {
				t.Fatalf("the pass did not use the memo: %+v", *shared.memo)
			}
		})
	}
}
