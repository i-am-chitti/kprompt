package intent

import "testing"

func TestNormalizeHPA(t *testing.T) {
	got := NormalizeHPA(Intent{Kind: KindUnknown, Target: Target{Name: "redis"}}, "add HPA for redis")
	if got.Kind != KindHPA {
		t.Fatalf("%+v", got)
	}
	if got.Target.Kind != "HorizontalPodAutoscaler" {
		t.Fatalf("%+v", got)
	}
	target, ok := got.StringParam("target")
	if !ok || target != "redis" {
		t.Fatalf("target=%v ok=%v", target, ok)
	}
	name, ok := got.StringParam("hpaName")
	if !ok || name != "redis-hpa" {
		t.Fatalf("hpaName=%v ok=%v", name, ok)
	}
}

func TestNormalizeHPADoesNotOverrideKEDA(t *testing.T) {
	got := NormalizeHPA(Intent{Kind: KindKEDA, Target: Target{Name: "api"}}, "add HPA for api")
	if got.Kind != KindKEDA {
		t.Fatalf("expected keda to win when already set: %+v", got)
	}
}

func TestLooksLikeHPAPrompt(t *testing.T) {
	if !LooksLikeHPAPrompt("add HPA for redis") {
		t.Fatal("expected match")
	}
	if !LooksLikeHPAPrompt("create HorizontalPodAutoscaler for api") {
		t.Fatal("expected hpa kind match")
	}
	if !LooksLikeHPAPrompt("autoscale api with cpu") {
		t.Fatal("expected cpu autoscale match")
	}
	if LooksLikeHPAPrompt("scale api to 3") {
		t.Fatal("plain replica scale should not match hpa")
	}
	if LooksLikeHPAPrompt("create a keda scaledobject for api") {
		t.Fatal("keda prompts should not match hpa")
	}
	if LooksLikeHPAPrompt("scale api to zero with keda") {
		t.Fatal("keda scale-to-zero should not match hpa")
	}
}
