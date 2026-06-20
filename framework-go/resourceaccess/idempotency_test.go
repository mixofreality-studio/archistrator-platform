package resourceaccess

import "testing"

func TestIdempotencyKeyIsString(t *testing.T) {
	var k IdempotencyKey = "wf-123:act-456"
	if string(k) != "wf-123:act-456" {
		t.Fatalf("want round-trip, got %q", k)
	}
	if k.IsZero() {
		t.Fatal("non-empty key should not be zero")
	}
	if !IdempotencyKey("").IsZero() {
		t.Fatal("empty key should be zero")
	}
}
