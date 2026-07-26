package intent

import "testing"

func TestNormalizeCleanup(t *testing.T) {
	got := NormalizeCleanup(Intent{Kind: KindUnknown}, "cleanup payments namespace")
	if got.Kind != KindCleanup {
		t.Fatalf("kind=%s", got.Kind)
	}
	got = NormalizeCleanup(Intent{Kind: KindGet}, "prune the cluster")
	if got.Kind != KindCleanup {
		t.Fatalf("kind=%s", got.Kind)
	}
	if scope, _ := got.StringParam("scope"); scope != "cluster" {
		t.Fatalf("scope=%v", got.Params)
	}
	got = NormalizeCleanup(Intent{Kind: KindScale}, "cleanup api")
	if got.Kind == KindCleanup {
		t.Fatal("should not override scale")
	}
}

func TestApplyCleanupScope(t *testing.T) {
	in := Intent{Kind: KindCleanup, Target: Target{Namespace: "default"}, Params: map[string]any{"scope": "cluster"}}
	got := ApplyCleanupScope(in, "cleanup my cluster", ScopePrefs{})
	if got.Target.Namespace != "" {
		t.Fatalf("ns=%q", got.Target.Namespace)
	}
	got = ApplyCleanupScope(in, "cleanup in the payments namespace", ScopePrefs{})
	if got.Target.Namespace != "payments" {
		t.Fatalf("ns=%q", got.Target.Namespace)
	}
}
