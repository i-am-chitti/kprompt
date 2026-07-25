package intent

import "testing"

func TestNormalizeInvestigate(t *testing.T) {
	got := NormalizeVerb(Intent{Kind: KindUnknown}, "investigate api in payments")
	if got.Kind != KindInvestigate {
		t.Fatalf("got %s", got.Kind)
	}
	got = NormalizeVerb(Intent{Kind: KindExplain}, "investigate payment-api root cause")
	if got.Kind != KindInvestigate {
		t.Fatalf("got %s", got.Kind)
	}
	// Crash explain stays explain (not investigate).
	got = NormalizeVerb(Intent{Kind: KindExplain}, "why is api crashing")
	if got.Kind != KindExplain {
		t.Fatalf("crash explain became %s", got.Kind)
	}
}
