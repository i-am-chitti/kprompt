package intent

import "testing"

func TestNormalizeTimeline(t *testing.T) {
	got := NormalizeVerb(Intent{Kind: KindUnknown}, "timeline for api")
	if got.Kind != KindTimeline || got.Target.Name != "api" {
		t.Fatalf("got %+v", got)
	}
	w, ok := got.Window()
	if !ok || w != "1h" {
		t.Fatalf("window=%q ok=%v", w, ok)
	}

	got = NormalizeVerb(Intent{Kind: KindGet}, "what happened to ledger")
	if got.Kind != KindTimeline || got.Target.Name != "ledger" {
		t.Fatalf("got %+v", got)
	}

	got = NormalizeVerb(Intent{Kind: KindExplain}, "why is api crashing")
	if got.Kind != KindWhy {
		t.Fatalf("crash why became %s", got.Kind)
	}
}
