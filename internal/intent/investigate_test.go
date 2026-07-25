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
	// Crash why is KindWhy (S-003), not investigate.
	got = NormalizeVerb(Intent{Kind: KindExplain}, "why is api crashing")
	if got.Kind != KindWhy {
		t.Fatalf("crash why became %s", got.Kind)
	}
}
