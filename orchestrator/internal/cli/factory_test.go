package cli

import "testing"

func TestNewSessionRejectsUnknownBackend(t *testing.T) {
	_, err := NewSession("nope", SessionOptions{RunID: "r", Role: "implementer"})
	if err == nil {
		t.Fatal("expected error")
	}
}
