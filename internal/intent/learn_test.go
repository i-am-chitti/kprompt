package intent

import "testing"

func TestNormalizeLearn(t *testing.T) {
	got := NormalizeLearn(Intent{Kind: KindUnknown}, "learn cluster tools")
	if got.Kind != KindLearn {
		t.Fatalf("kind = %s", got.Kind)
	}
	got = NormalizeLearn(Intent{Kind: KindGet}, "detect tools")
	if got.Kind != KindLearn {
		t.Fatalf("kind = %s", got.Kind)
	}
	got = NormalizeLearn(Intent{Kind: KindScale}, "learn cluster")
	if got.Kind != KindScale {
		t.Fatalf("scale should stay: %s", got.Kind)
	}
	got = NormalizeLearn(Intent{Kind: KindUnknown}, "learn")
	if got.Kind != KindLearn {
		t.Fatalf("bare learn: %s", got.Kind)
	}
}
