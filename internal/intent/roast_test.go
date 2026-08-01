package intent

import "testing"

func TestNormalizeRoast(t *testing.T) {
	got := NormalizeRoast(Intent{Kind: KindUnknown}, "how's my cluster")
	if got.Kind != KindRoast {
		t.Fatalf("kind=%s", got.Kind)
	}
	if scope, _ := got.StringParam("scope"); scope != "cluster" {
		t.Fatalf("scope=%v params=%v", scope, got.Params)
	}

	got = NormalizeRoast(Intent{Kind: KindGet}, "roast my namespace")
	if got.Kind != KindRoast {
		t.Fatalf("kind=%s", got.Kind)
	}

	got = NormalizeRoast(Intent{Kind: KindUnknown}, "vibe check the cluster")
	if got.Kind != KindRoast {
		t.Fatalf("kind=%s", got.Kind)
	}

	got = NormalizeRoast(Intent{Kind: KindScale}, "how's my cluster")
	if got.Kind == KindRoast {
		t.Fatal("should not remap scale")
	}
}

func TestLooksLikeRoastPrompt(t *testing.T) {
	yes := []string{
		"how's my cluster",
		"how is my namespace",
		"roast my cluster",
		"rate my namespace",
		"cluster vibe check",
	}
	for _, p := range yes {
		if !LooksLikeRoastPrompt(p) {
			t.Fatalf("expected match for %q", p)
		}
	}
	no := []string{
		"how many pods",
		"optimize my cluster",
		"scale api to 3",
		"show pods",
	}
	for _, p := range no {
		if LooksLikeRoastPrompt(p) {
			t.Fatalf("unexpected match for %q", p)
		}
	}
}

func TestApplyRoastScope(t *testing.T) {
	in := NormalizeRoast(Intent{Kind: KindUnknown, Raw: "how's my cluster"}, "how's my cluster")
	got := ApplyRoastScope(in, "how's my cluster", ScopePrefs{})
	if got.Target.Namespace != "" {
		t.Fatalf("cluster roast should clear ns, got %q", got.Target.Namespace)
	}

	in = Intent{Kind: KindRoast, Target: Target{Namespace: "default"}, Params: map[string]any{"scope": "cluster"}}
	got = ApplyRoastScope(in, "how's my cluster", ScopePrefs{ForceNamespace: true, DefaultNamespace: "payments"})
	if scope, ok := got.StringParam("scope"); ok {
		t.Fatalf("forced -n should drop cluster scope, got %q", scope)
	}
}
