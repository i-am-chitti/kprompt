package intent

import "testing"

func TestNormalizeScore(t *testing.T) {
	got := NormalizeScore(Intent{Kind: KindUnknown}, "score payments namespace")
	if got.Kind != KindScore {
		t.Fatalf("kind=%s", got.Kind)
	}
	got = NormalizeScore(Intent{Kind: KindOptimize}, "scorecard for the cluster")
	if got.Kind != KindScore {
		t.Fatalf("kind=%s", got.Kind)
	}
	if scope, _ := got.StringParam("scope"); scope != "cluster" {
		t.Fatalf("scope=%q", scope)
	}
	got = NormalizeScore(Intent{Kind: KindUnknown}, "how's my cluster")
	if got.Kind == KindScore {
		t.Fatal("roast phrasing must not become score")
	}
	got = NormalizeScore(Intent{Kind: KindScale}, "score my cluster")
	if got.Kind == KindScore {
		t.Fatal("scale should not remap")
	}
}

func TestApplyScoreScope(t *testing.T) {
	in := Intent{Kind: KindScore, Target: Target{Namespace: "default"}, Params: map[string]any{"scope": "cluster"}}
	got := ApplyScoreScope(in, "score my cluster", ScopePrefs{})
	if got.Target.Namespace != "" {
		t.Fatalf("namespace=%q", got.Target.Namespace)
	}
	got = ApplyScoreScope(in, "score in the payments namespace", ScopePrefs{})
	if got.Target.Namespace != "payments" {
		t.Fatalf("namespace=%q", got.Target.Namespace)
	}
}
