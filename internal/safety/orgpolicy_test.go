package safety

import (
	"testing"
	"time"

	"github.com/kprompt/kprompt/internal/intent"
	"github.com/kprompt/kprompt/internal/planner"
)

func TestApplyOrgPolicyNamespaceDeny(t *testing.T) {
	plan := planner.ExecutionPlan{
		Intent: intent.Intent{
			Kind: intent.KindScale,
			Target: intent.Target{Namespace: "kube-system", Name: "coredns"},
		},
	}
	base := EvaluatePlan(plan)
	org := &OrgPolicy{
		MaxRisk:         "high",
		DenyNamespaces:  []string{"kube-system"},
		AllowNamespaces: []string{"*"},
	}
	r := ApplyOrgPolicy(base, plan, org, "")
	if !r.Denied {
		t.Fatalf("expected deny: %+v", r)
	}
}

func TestApplyOrgPolicyAllowList(t *testing.T) {
	plan := planner.ExecutionPlan{
		Intent: intent.Intent{
			Kind: intent.KindScale,
			Target: intent.Target{Namespace: "prod", Name: "api"},
		},
	}
	base := EvaluatePlan(plan)
	org := &OrgPolicy{
		MaxRisk:         "high",
		AllowNamespaces: []string{"staging"},
	}
	r := ApplyOrgPolicy(base, plan, org, "")
	if !r.Denied {
		t.Fatalf("expected deny outside allow list: %+v", r)
	}
}

func TestApplyOrgPolicyMaxRisk(t *testing.T) {
	plan := planner.ExecutionPlan{
		Intent: intent.Intent{Kind: intent.KindDelete},
		Actions: []planner.Action{{
			Op: planner.OpDelete,
			Object: planner.ObjectRef{Kind: "Deployment", Name: "redis", Namespace: "default"},
		}},
	}
	base := EvaluatePlan(plan) // RiskHigh
	org := &OrgPolicy{MaxRisk: "medium", AllowNamespaces: []string{"*"}}
	r := ApplyOrgPolicy(base, plan, org, "")
	if !r.Denied {
		t.Fatalf("expected max_risk deny: %+v base=%+v", r, base)
	}
}

func TestApplyOrgPolicyDenyIntent(t *testing.T) {
	plan := planner.ExecutionPlan{
		Intent: intent.Intent{
			Kind:   intent.KindScale,
			Target: intent.Target{Namespace: "default", Name: "api"},
		},
	}
	base := EvaluatePlan(plan)
	org := &OrgPolicy{
		MaxRisk:         "high",
		DenyIntents:     []string{"wipe", "delete_cluster", "scale"},
		AllowNamespaces: []string{"*"},
	}
	r := ApplyOrgPolicy(base, plan, org, "")
	if !r.Denied {
		t.Fatalf("expected scale deny: %+v", r)
	}
}

func TestApplyOrgPolicyNilPassthrough(t *testing.T) {
	plan := planner.ExecutionPlan{Intent: intent.Intent{Kind: intent.KindGet}}
	base := EvaluatePlan(plan)
	r := ApplyOrgPolicy(base, plan, nil, "")
	if r != base {
		t.Fatalf("nil org should passthrough")
	}
}

func TestChangeWindowDeniesOutsideHours(t *testing.T) {
	plan := planner.ExecutionPlan{
		Intent:            intent.Intent{Kind: intent.KindScale, Target: intent.Target{Namespace: "default", Name: "api"}},
		RequiresApproval:  true,
	}
	base := EvaluatePlan(plan)
	org := &OrgPolicy{
		MaxRisk:         "high",
		AllowNamespaces: []string{"*"},
		ChangeWindows: []ChangeWindow{{
			Contexts: []string{"prod*"},
			TZ:       "UTC",
			Days:     []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"},
			Start:    "09:00",
			End:      "17:00",
		}},
	}
	// Monday 08:00 UTC — before window
	now := time.Date(2026, 7, 27, 8, 0, 0, 0, time.UTC)
	r := ApplyOrgPolicyAt(base, plan, org, "prod-west", now)
	if !r.Denied {
		t.Fatalf("expected outside-window deny: %+v", r)
	}
	// Monday 10:00 UTC — inside
	now = time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	r = ApplyOrgPolicyAt(base, plan, org, "prod-west", now)
	if r.Denied {
		t.Fatalf("expected allow inside window: %+v", r)
	}
	// Unrelated context — no window claims it
	now = time.Date(2026, 7, 27, 8, 0, 0, 0, time.UTC)
	r = ApplyOrgPolicyAt(base, plan, org, "staging", now)
	if r.Denied {
		t.Fatalf("unmatched context should pass: %+v", r)
	}
	// Read stays allowed outside window
	read := planner.ExecutionPlan{Intent: intent.Intent{Kind: intent.KindGet}}
	rb := EvaluatePlan(read)
	r = ApplyOrgPolicyAt(rb, read, org, "prod-west", now)
	if r.Denied {
		t.Fatalf("reads should pass outside window: %+v", r)
	}
}
