package planner

import (
	"strings"
	"testing"

	"github.com/kprompt/kprompt/internal/intent"
)

func TestBuildHPA(t *testing.T) {
	plan, err := Build(intent.Intent{
		Kind: intent.KindHPA,
		Target: intent.Target{
			Name:      "redis",
			Namespace: "default",
			Kind:      "HorizontalPodAutoscaler",
		},
		Params: map[string]any{
			"minReplicas": 2,
			"maxReplicas": 8,
			"cpu":         60,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Op != OpCreate || plan.Actions[0].Backend != "kubernetes" {
		t.Fatalf("%+v", plan)
	}
	if !plan.RequiresApproval {
		t.Fatal("expected approval")
	}
	m := plan.Actions[0].Manifest
	if !strings.Contains(m, "kind: HorizontalPodAutoscaler") || !strings.Contains(m, "autoscaling/v2") {
		t.Fatalf("manifest=%s", m)
	}
	if !strings.Contains(m, "name: redis") || !strings.Contains(m, "name: redis-hpa") {
		t.Fatalf("manifest=%s", m)
	}
	if plan.Actions[0].Object.Kind != "HorizontalPodAutoscaler" || plan.Actions[0].Object.Name != "redis-hpa" {
		t.Fatalf("%+v", plan.Actions[0].Object)
	}
}

func TestBuildHPARequiresTarget(t *testing.T) {
	_, err := Build(intent.Intent{Kind: intent.KindHPA})
	if err == nil {
		t.Fatal("expected error")
	}
}
