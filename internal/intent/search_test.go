package intent

import "testing"

func TestNormalizeSearch(t *testing.T) {
	got := NormalizeSearch(Intent{Kind: KindUnknown}, "find every Deployment using redis")
	if got.Kind != KindSearch {
		t.Fatalf("kind=%s", got.Kind)
	}
	if q, _ := got.StringParam("query"); q != "redis" {
		t.Fatalf("query=%q", q)
	}
	if got.Target.Kind != "Deployment" {
		t.Fatalf("kind filter=%q", got.Target.Kind)
	}

	got = NormalizeSearch(Intent{Kind: KindGet}, "search for postgres")
	if got.Kind != KindSearch {
		t.Fatalf("kind=%s", got.Kind)
	}
	if q, _ := got.StringParam("query"); q != "postgres" {
		t.Fatalf("query=%q", q)
	}

	got = NormalizeSearch(Intent{Kind: KindGet}, "which deployments use redis")
	if got.Kind != KindSearch {
		t.Fatalf("kind=%s", got.Kind)
	}

	got = NormalizeSearch(Intent{Kind: KindUnknown}, "find unused configmaps")
	if got.Kind == KindSearch {
		t.Fatal("cleanup phrasing must not become search")
	}

	got = NormalizeSearch(Intent{Kind: KindScale}, "find every Deployment using redis")
	if got.Kind == KindSearch {
		t.Fatal("scale should not be remapped to search")
	}
}

func TestApplySearchScope(t *testing.T) {
	in := Intent{Kind: KindSearch, Target: Target{Namespace: "default"}, Params: map[string]any{"scope": "cluster"}}
	got := ApplySearchScope(in, "search the cluster for redis", ScopePrefs{})
	if got.Target.Namespace != "" {
		t.Fatalf("namespace=%q", got.Target.Namespace)
	}
	got = ApplySearchScope(in, "search for redis in the payments namespace", ScopePrefs{})
	if got.Target.Namespace != "payments" {
		t.Fatalf("namespace=%q", got.Target.Namespace)
	}
}

func TestLooksLikeSearchPrompt(t *testing.T) {
	if !LooksLikeSearchPrompt("find every Deployment using redis") {
		t.Fatal("expected search")
	}
	if LooksLikeSearchPrompt("find unused secrets") {
		t.Fatal("cleanup should not look like search")
	}
}
