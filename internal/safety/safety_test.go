package safety

import (
	"strings"
	"testing"

	"github.com/kprompt/kprompt/internal/intent"
	"github.com/kprompt/kprompt/internal/planner"
)

func TestCheckPromptDeniesWipe(t *testing.T) {
	cases := []string{
		`remove my f*cking cluster`,
		`delete the cluster`,
		`wipe the cluster now`,
		`delete all namespaces`,
		`destroy everything in the cluster`,
		`delete the namespace`,
		`delete all pods`,
	}
	for _, c := range cases {
		r := CheckPrompt(c)
		if !r.Denied {
			t.Fatalf("expected deny for %q", c)
		}
		if r.Risk != RiskDenied {
			t.Fatalf("expected risk denied for %q, got %s", c, r.Risk)
		}
	}
}

func TestCheckPromptAllowsScale(t *testing.T) {
	r := CheckPrompt(`scale api to 10`)
	if r.Denied {
		t.Fatal("scale should not be denied")
	}
}

func TestCheckPromptAllowsNamedDelete(t *testing.T) {
	r := CheckPrompt(`delete deployment redis`)
	if r.Denied {
		t.Fatal("named delete should not be hard-denied at prompt layer")
	}
}

func TestEvaluatePlanDelete(t *testing.T) {
	ok := EvaluatePlan(planner.ExecutionPlan{
		Intent: intent.Intent{Kind: intent.KindDelete},
		Actions: []planner.Action{{
			Op: planner.OpDelete,
			Object: planner.ObjectRef{
				Kind: "Deployment", Name: "redis", Namespace: "default",
			},
		}},
	})
	if ok.Denied || ok.Risk != RiskHigh {
		t.Fatalf("%+v", ok)
	}

	denied := EvaluatePlan(planner.ExecutionPlan{
		Intent: intent.Intent{Kind: intent.KindDelete},
		Actions: []planner.Action{{
			Op: planner.OpDelete,
			Object: planner.ObjectRef{
				Kind: "Namespace", Name: "prod",
			},
		}},
	})
	if !denied.Denied {
		t.Fatal("expected namespace delete denied")
	}

	unscoped := EvaluatePlan(planner.ExecutionPlan{
		Intent: intent.Intent{Kind: intent.KindDelete},
		Actions: []planner.Action{{
			Op: planner.OpDelete,
			Object: planner.ObjectRef{Kind: "Pod", Name: "all"},
		}},
	})
	if !unscoped.Denied {
		t.Fatal("expected unscoped denied")
	}

	jobOK := EvaluatePlan(planner.ExecutionPlan{
		Intent: intent.Intent{Kind: intent.KindDelete},
		Actions: []planner.Action{{
			Op: planner.OpDelete,
			Object: planner.ObjectRef{Kind: "Job", Name: "old-migrate", Namespace: "payments"},
		}},
	})
	if jobOK.Denied || jobOK.Risk != RiskHigh {
		t.Fatalf("Job delete should be RiskHigh: %+v", jobOK)
	}

	cmDenied := EvaluatePlan(planner.ExecutionPlan{
		Intent: intent.Intent{Kind: intent.KindDelete},
		Actions: []planner.Action{{
			Op: planner.OpDelete,
			Object: planner.ObjectRef{Kind: "ConfigMap", Name: "orphan", Namespace: "payments"},
		}},
	})
	if !cmDenied.Denied {
		t.Fatal("expected ConfigMap delete denied")
	}
	if !strings.Contains(cmDenied.Message, "confirm_orphans") {
		t.Fatalf("deny message should mention confirm_orphans: %q", cmDenied.Message)
	}

	cmOK := EvaluatePlan(planner.ExecutionPlan{
		Intent: intent.Intent{
			Kind: intent.KindDelete,
			Params: map[string]any{"confirm_orphans": true, "reason": "CleanupOrphans"},
		},
		Actions: []planner.Action{{
			Op: planner.OpDelete,
			Object: planner.ObjectRef{Kind: "ConfigMap", Name: "orphan", Namespace: "payments"},
		}},
	})
	if cmOK.Denied || cmOK.Risk != RiskHigh {
		t.Fatalf("ConfigMap delete with confirm_orphans should be RiskHigh: %+v", cmOK)
	}

	secretOK := EvaluatePlan(planner.ExecutionPlan{
		Intent: intent.Intent{
			Kind:   intent.KindDelete,
			Params: map[string]any{"confirm_orphans": "true"},
		},
		Actions: []planner.Action{{
			Op: planner.OpDelete,
			Object: planner.ObjectRef{Kind: "Secret", Name: "orphan", Namespace: "payments"},
		}},
	})
	if secretOK.Denied || secretOK.Risk != RiskHigh {
		t.Fatalf("Secret delete with confirm_orphans=true string should be RiskHigh: %+v", secretOK)
	}
}

func TestEvaluatePlanAllowsSecretGet(t *testing.T) {
	r := EvaluatePlan(planner.ExecutionPlan{
		Intent: intent.Intent{Kind: intent.KindGet},
		Actions: []planner.Action{{
			Op: planner.OpGet,
			Object: planner.ObjectRef{
				Kind: "Secret", Name: "db", Namespace: "default",
			},
		}},
	})
	if r.Denied || r.Risk != RiskLow {
		t.Fatalf("secret get should be RiskLow, got %+v", r)
	}
}

func TestCheckPromptAllowsShowSecrets(t *testing.T) {
	r := CheckPrompt(`show secrets in prod`)
	if r.Denied {
		t.Fatal("listing secrets must not be hard-denied")
	}
}
