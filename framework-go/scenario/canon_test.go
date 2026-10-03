package scenario

import "testing"

func TestNormalize(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"settlementManager", "settlementmanager"},
		{"settlement-manager", "settlementmanager"},
		{"Settlement_Manager ", "settlementmanager"},
	} {
		if got := Normalize(tc.in); got != tc.want {
			t.Errorf("Normalize(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestCanonicalJSONSortsAndIsStable(t *testing.T) {
	a := []Scenario{{ID: "UC2-P1", UseCase: "UC2"}, {ID: "UC1-P2", UseCase: "UC1"}, {ID: "UC1-P1", UseCase: "UC1"}}
	b := []Scenario{{ID: "UC1-P1", UseCase: "UC1"}, {ID: "UC1-P2", UseCase: "UC1"}, {ID: "UC2-P1", UseCase: "UC2"}}
	ja, err := CanonicalJSON(a)
	if err != nil {
		t.Fatal(err)
	}
	jb, _ := CanonicalJSON(b)
	if string(ja) != string(jb) {
		t.Fatalf("order-dependent output:\n%s\n%s", ja, jb)
	}
	if ja[len(ja)-1] != '\n' {
		t.Fatal("not newline terminated")
	}
	fa, _ := Fingerprint(a)
	fb, _ := Fingerprint(b)
	if fa != fb || len(fa) != 64 {
		t.Fatalf("fingerprint %q vs %q", fa, fb)
	}
}

func TestCanonicalJSONNilAndEmptyEncodeAlikeWithoutMutatingInput(t *testing.T) {
	withNil := []Scenario{{ID: "UC1-P1", Stimuli: []Stimulus{{Seq: 1, Node: "n1"}}}}
	withEmpty := []Scenario{{ID: "UC1-P1", Path: []string{}, Guards: []Guard{},
		Preconditions: Outcome{ExpectedOutputs: []Output{}, ExpectedStates: []State{}, Hints: []string{}},
		Stimuli:       []Stimulus{{Seq: 1, Node: "n1", Outcome: Outcome{ExpectedOutputs: []Output{}, ExpectedStates: []State{}, Hints: []string{}}}}}}
	ja, err := CanonicalJSON(withNil)
	if err != nil {
		t.Fatal(err)
	}
	jb, err := CanonicalJSON(withEmpty)
	if err != nil {
		t.Fatal(err)
	}
	if string(ja) != string(jb) {
		t.Fatalf("nil and empty slices encode differently:\n%s\n%s", ja, jb)
	}
	if withNil[0].Path != nil || withNil[0].Stimuli[0].Hints != nil {
		t.Fatal("CanonicalJSON wrote through to its input")
	}
}
