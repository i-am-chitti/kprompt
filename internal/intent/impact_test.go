package intent

import "testing"

func TestNormalizeImpact(t *testing.T) {
	tests := []struct {
		prompt string
		name   string
		kind   string
	}{
		{"impact of service redis", "redis", "Service"},
		{"who consumes api", "api", "Service"},
		{"what depends on deployment checkout", "checkout", "Deployment"},
		{"blast radius for payment-api", "payment-api", "Service"},
	}
	for _, tc := range tests {
		got := NormalizeVerb(Intent{Kind: KindUnknown}, tc.prompt)
		if got.Kind != KindImpact || got.Target.Name != tc.name || got.Target.Kind != tc.kind {
			t.Errorf("%q: got %+v", tc.prompt, got)
		}
	}
}

func TestNormalizeImpactLeavesMutationAlone(t *testing.T) {
	got := NormalizeVerb(Intent{
		Kind:   KindScale,
		Target: Target{Name: "api", Kind: "Deployment"},
	}, "scale api to 4 and show impact")
	if got.Kind != KindScale {
		t.Fatalf("kind=%s", got.Kind)
	}
}
