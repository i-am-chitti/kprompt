package v1

import "testing"

func TestDefaultMode(t *testing.T) {
	if DefaultMode("") != ModeObserve {
		t.Fatal("empty → Observe")
	}
	if DefaultMode(ModeObserve) != ModeObserve {
		t.Fatal("observe")
	}
}
