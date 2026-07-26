package suggest

import (
	"context"
	"testing"

	"github.com/kprompt/kprompt/internal/incident"
	"github.com/kprompt/kprompt/internal/planner"
)

func TestFromCleanupDeletesJobsAndReplicaSets(t *testing.T) {
	inv := incident.Investigation{
		Namespace: "payments",
		Findings: []incident.Finding{
			auditFinding("Cleanup.CompletedJob", "Job/old-migrate finished 2d ago", "Job", "old-migrate", "payments"),
			auditFinding("Cleanup.OldReplicaSet", "ReplicaSet/api-old has 0 replicas", "ReplicaSet", "api-old", "payments"),
			auditFinding("Cleanup.UnusedConfigMap", "ConfigMap/orphan-config appears unused", "ConfigMap", "orphan-config", "payments"),
			auditFinding("Cleanup.UnusedSecret", "Secret/orphan-secret appears unused", "Secret", "orphan-secret", "payments"),
		},
	}
	suggestions, err := FromCleanup(context.Background(), inv)
	if err != nil {
		t.Fatal(err)
	}
	actionable := ActionablePlans(suggestions)
	if len(actionable) != 1 {
		t.Fatalf("want 1 delete plan, got %d: %+v", len(actionable), suggestions)
	}
	plan := actionable[0].Plan
	if len(plan.Actions) != 2 {
		t.Fatalf("want 2 delete actions, got %d", len(plan.Actions))
	}
	for _, a := range plan.Actions {
		if a.Op != planner.OpDelete {
			t.Fatalf("op=%s", a.Op)
		}
		switch a.Object.Kind {
		case "Job", "ReplicaSet":
		default:
			t.Fatalf("unexpected kind %s", a.Object.Kind)
		}
	}
	if len(suggestions) < 3 {
		t.Fatalf("expected guidance for ConfigMap/Secret: %+v", suggestions)
	}
	for _, s := range suggestions {
		if s.Code == "Cleanup.UnusedConfigMap" || s.Code == "Cleanup.UnusedSecret" {
			if s.Plan != nil {
				t.Fatalf("%s must be guidance-only", s.Code)
			}
		}
	}
}

func TestFromCleanupGuidanceOnlyWhenNoJobs(t *testing.T) {
	inv := incident.Investigation{
		Namespace: "payments",
		Findings: []incident.Finding{
			auditFinding("Cleanup.UnusedConfigMap", "ConfigMap/orphan appears unused", "ConfigMap", "orphan", "payments"),
		},
	}
	suggestions, err := FromCleanup(context.Background(), inv)
	if err != nil {
		t.Fatal(err)
	}
	if len(ActionablePlans(suggestions)) != 0 {
		t.Fatalf("ConfigMap-only must not produce a delete plan: %+v", suggestions)
	}
}
