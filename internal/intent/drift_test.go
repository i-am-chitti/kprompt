package intent

import "testing"

func TestNormalizeDrift(t *testing.T) {
	got := NormalizeDrift(Intent{Kind: KindUnknown}, "check cluster drift")
	if got.Kind != KindDrift {
		t.Fatalf("kind=%s", got.Kind)
	}
	if scope, _ := got.StringParam("scope"); scope != "cluster" {
		t.Fatalf("scope=%v", got.Params)
	}
	got = NormalizeDrift(Intent{Kind: KindGitOps}, "show drift vs git")
	if got.Kind != KindDrift {
		t.Fatalf("gitops→drift: %s", got.Kind)
	}
	got = NormalizeDrift(Intent{Kind: KindGet}, "what is out of sync")
	if got.Kind != KindDrift {
		t.Fatalf("out of sync: %s", got.Kind)
	}
	got = NormalizeDrift(Intent{Kind: KindScale}, "check drift")
	if got.Kind != KindScale {
		t.Fatalf("scale should stay: %s", got.Kind)
	}
	if LooksLikeDriftPrompt("show gitops sync status") {
		t.Fatal("plain gitops status must not look like drift")
	}
}
