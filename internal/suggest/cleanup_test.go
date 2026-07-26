package suggest

import (
	"context"
	"strings"
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
	suggestions, err := FromCleanup(context.Background(), inv, "cleanup payments namespace")
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
	suggestions, err := FromCleanup(context.Background(), inv, "cleanup payments")
	if err != nil {
		t.Fatal(err)
	}
	if len(ActionablePlans(suggestions)) != 0 {
		t.Fatalf("ConfigMap-only must not produce a delete plan: %+v", suggestions)
	}
}

func TestFromCleanupOrphansGuidanceWithoutConfirm(t *testing.T) {
	inv := incident.Investigation{
		Namespace: "payments",
		Findings: []incident.Finding{
			auditFinding("Cleanup.UnusedConfigMap", "ConfigMap/orphan-config appears unused", "ConfigMap", "orphan-config", "payments"),
			auditFinding("Cleanup.UnusedSecret", "Secret/orphan-secret appears unused", "Secret", "orphan-secret", "payments"),
		},
	}
	suggestions, err := FromCleanup(context.Background(), inv, "cleanup payments find unused")
	if err != nil {
		t.Fatal(err)
	}
	if len(ActionablePlans(suggestions)) != 0 {
		t.Fatalf("without confirm phrase CM/Secret must be guidance-only: %+v", suggestions)
	}
	var sawCM, sawSecret bool
	for _, s := range suggestions {
		if s.Code == "Cleanup.UnusedConfigMap" {
			sawCM = true
			if s.Plan != nil {
				t.Fatal("ConfigMap must be guidance-only")
			}
		}
		if s.Code == "Cleanup.UnusedSecret" {
			sawSecret = true
			if s.Plan != nil {
				t.Fatal("Secret must be guidance-only")
			}
		}
	}
	if !sawCM || !sawSecret {
		t.Fatalf("expected CM and Secret guidance: %+v", suggestions)
	}
}

func TestFromCleanupOrphansDeleteWithConfirm(t *testing.T) {
	inv := incident.Investigation{
		Namespace: "payments",
		Findings: []incident.Finding{
			auditFinding("Cleanup.CompletedJob", "Job/old-migrate finished 2d ago", "Job", "old-migrate", "payments"),
			auditFinding("Cleanup.UnusedConfigMap", "ConfigMap/orphan-config appears unused", "ConfigMap", "orphan-config", "payments"),
			auditFinding("Cleanup.UnusedSecret", "Secret/orphan-secret appears unused", "Secret", "orphan-secret", "payments"),
		},
	}
	suggestions, err := FromCleanup(context.Background(), inv, "cleanup payments and confirm orphans")
	if err != nil {
		t.Fatal(err)
	}
	actionable := ActionablePlans(suggestions)
	if len(actionable) != 2 {
		t.Fatalf("want Job/RS + orphan plans, got %d: %+v", len(actionable), suggestions)
	}
	if actionable[0].Code != "Cleanup.Delete" {
		t.Fatalf("first plan should be Job/RS, got %s", actionable[0].Code)
	}
	orphan := actionable[1]
	if orphan.Code != "Cleanup.DeleteOrphans" {
		t.Fatalf("second plan should be orphans, got %s", orphan.Code)
	}
	plan := orphan.Plan
	if plan == nil || !plan.RequiresApproval {
		t.Fatal("orphan plan must require approval")
	}
	if reason, _ := plan.Intent.Params["reason"].(string); reason != "CleanupOrphans" {
		t.Fatalf("reason=%v", plan.Intent.Params["reason"])
	}
	if !IsCleanupOrphanPlan(*plan) {
		t.Fatal("expected confirm_orphans truthy")
	}
	if len(plan.Actions) != 2 {
		t.Fatalf("want 2 orphan deletes, got %d", len(plan.Actions))
	}
	for _, a := range plan.Actions {
		if a.Op != planner.OpDelete {
			t.Fatalf("op=%s", a.Op)
		}
		switch a.Object.Kind {
		case "ConfigMap", "Secret":
		default:
			t.Fatalf("unexpected kind %s", a.Object.Kind)
		}
	}
	if !strings.Contains(plan.Summary, "confirm_orphans") {
		t.Fatalf("summary should mention confirm_orphans: %q", plan.Summary)
	}
}
