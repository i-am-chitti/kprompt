package intent

import "testing"

func TestNormalizeAudit(t *testing.T) {
	got := NormalizeAudit(Intent{Kind: KindUnknown}, "audit payments namespace")
	if got.Kind != KindAudit {
		t.Fatalf("kind=%s", got.Kind)
	}
	got = NormalizeAudit(Intent{Kind: KindGet}, "security scan the cluster")
	if got.Kind != KindAudit {
		t.Fatalf("kind=%s", got.Kind)
	}
	if scope, _ := got.StringParam("scope"); scope != "cluster" {
		t.Fatalf("scope=%v", got.Params)
	}
	got = NormalizeAudit(Intent{Kind: KindScale}, "audit api")
	if got.Kind == KindAudit {
		t.Fatal("should not override scale")
	}
}

func TestApplyAuditScope(t *testing.T) {
	in := Intent{Kind: KindAudit, Target: Target{Namespace: "default"}, Params: map[string]any{"scope": "cluster"}}
	got := ApplyAuditScope(in, "audit my cluster", ScopePrefs{})
	if got.Target.Namespace != "" {
		t.Fatalf("ns=%q", got.Target.Namespace)
	}
	got = ApplyAuditScope(in, "audit in the payments namespace", ScopePrefs{})
	if got.Target.Namespace != "payments" {
		t.Fatalf("ns=%q", got.Target.Namespace)
	}
}
