package goodeng

import "testing"

func TestCompute(t *testing.T) {
	g := &goodEngine{}
	if v, err := g.Compute(1); err != nil || v != 2 {
		t.Fatalf("Compute(1) = %d, %v", v, err)
	}
}
