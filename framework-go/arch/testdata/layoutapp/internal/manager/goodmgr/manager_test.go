package goodmgr

import "testing"

func TestDo(t *testing.T) {
	w := &wfs{}
	if err := w.Do(); err != nil {
		t.Fatal(err)
	}
}
