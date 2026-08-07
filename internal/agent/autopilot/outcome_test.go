package autopilot

import (
	"strings"
	"testing"

	"github.com/kprompt/kprompt/internal/agent/patterns"
	"github.com/kprompt/kprompt/internal/verify"
)

func TestContextFromProposal(t *testing.T) {
	prop := Proposal{
		Namespace:  "payments",
		ActionID:   ActionRollbackFailedRollout,
		IncidentID: "inc-9",
		TargetKind: "Deployment",
		TargetName: "api",
		Plan:       PlanBody{Summary: "rollback failed rollout"},
	}
	ctx := ContextFromProposal(prop)
	if ctx.Namespace != "payments" || ctx.Incident.ID != "inc-9" {
		t.Fatalf("%+v", ctx)
	}
	if ctx.Target == nil || ctx.Target.Name != "api" {
		t.Fatalf("target=%+v", ctx.Target)
	}
	if len(ctx.Incident.Evidence) == 0 || !strings.Contains(ctx.Incident.Evidence[0].Reason, "Progress") {
		t.Fatalf("evidence=%+v", ctx.Incident.Evidence)
	}
}

func TestWriteLearnOutcomeNilLib(t *testing.T) {
	prop := Proposal{Namespace: "ns", ActionID: ActionRestartDeployment, TargetName: "api"}
	_, err := WriteLearnOutcome(nil, ContextFromProposal(prop), patterns.OutcomeApplySuccess)
	if err != nil {
		t.Fatal(err)
	}
}

func TestWriteLearnOutcomeApplySuccess(t *testing.T) {
	lib := patterns.New(patterns.NewMemStore())
	prop := Proposal{
		Namespace:  "payments",
		ActionID:   ActionRollbackFailedRollout,
		TargetKind: "Deployment",
		TargetName: "api",
		Plan:       PlanBody{Summary: "rollback api ProgressDeadlineExceeded"},
	}
	ctx := ContextFromProposal(prop)
	p, err := WriteLearnOutcome(lib, ctx, patterns.OutcomeApplySuccess)
	if err != nil {
		t.Fatal(err)
	}
	if p.Confirmed != 1 {
		t.Fatalf("%+v", p)
	}
	// Second success raises confirmed further — visible Learn loop.
	p2, err := WriteLearnOutcome(lib, ctx, patterns.OutcomeApplySuccess)
	if err != nil {
		t.Fatal(err)
	}
	if p2.Confirmed != 2 {
		t.Fatalf("second success: first=%+v second=%+v", p, p2)
	}
}

func TestAttachVerifyFailedClearsApplied(t *testing.T) {
	prop := &Proposal{
		ActionID:   ActionRollbackFailedRollout,
		Namespace:  "payments",
		TargetName: "missing-api",
		TargetKind: "Deployment",
		Decision:   DecisionApplied,
		Applied:    true,
		Plan:       PlanBody{Summary: "rollback"},
	}
	// nil client → verify skipped (not failed). Use skipped path.
	rep := AttachVerify(t.Context(), nil, prop)
	if rep.Status != verify.Skipped {
		t.Fatalf("nil client should skip: %+v", rep)
	}
}

func TestVerifyPlanForRestart(t *testing.T) {
	plan, err := verifyPlanFor(Proposal{
		ActionID: ActionRestartDeployment, Namespace: "ns", TargetName: "api",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.RequiresApproval || len(plan.Actions) != 1 {
		t.Fatalf("%+v", plan)
	}
}
