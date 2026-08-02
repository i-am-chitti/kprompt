package intent

import "testing"

func TestNormalizeArchitecture(t *testing.T) {
	got := NormalizeArchitecture(Intent{Kind: KindUnknown}, "explain architecture")
	if got.Kind != KindArchitecture {
		t.Fatalf("kind=%s", got.Kind)
	}
	got = NormalizeArchitecture(Intent{Kind: KindExplain}, "what does this cluster look like")
	if got.Kind != KindArchitecture {
		t.Fatalf("kind=%s", got.Kind)
	}
	if scope, _ := got.StringParam("scope"); scope != "cluster" {
		t.Fatalf("scope=%q", scope)
	}
	got = NormalizeArchitecture(Intent{Kind: KindGet}, "show service dependency graph")
	if got.Kind == KindArchitecture {
		t.Fatal("graph phrasing must not become architecture")
	}
	got = NormalizeArchitecture(Intent{Kind: KindScale}, "explain architecture")
	if got.Kind == KindArchitecture {
		t.Fatal("scale should not remap")
	}
}

func TestApplyArchitectureScope(t *testing.T) {
	in := Intent{Kind: KindArchitecture, Target: Target{Namespace: "default"}, Params: map[string]any{"scope": "cluster"}}
	got := ApplyArchitectureScope(in, "cluster architecture overview", ScopePrefs{})
	if got.Target.Namespace != "" {
		t.Fatalf("namespace=%q", got.Target.Namespace)
	}
	got = ApplyArchitectureScope(in, "explain architecture in the payments namespace", ScopePrefs{})
	if got.Target.Namespace != "payments" {
		t.Fatalf("namespace=%q", got.Target.Namespace)
	}
}
