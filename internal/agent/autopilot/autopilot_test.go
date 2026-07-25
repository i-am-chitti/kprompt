package autopilot

import (
	"strings"
	"testing"

	"github.com/kprompt/kprompt/internal/agent/ctxbuild"
	"github.com/kprompt/kprompt/internal/incident"
)

func TestHardDenyOutsideAllowlist(t *testing.T) {
	dec, reason := EvaluateAction(DefaultPolicy(), "deleteNamespace")
	if dec != DecisionDenied || !strings.Contains(reason, "MVP allowlist") {
		t.Fatalf("%s %s", dec, reason)
	}
}

func TestPolicyAllowSubset(t *testing.T) {
	pol := Policy{Allow: nil}
	dec, _ := EvaluateAction(pol, ActionRollbackFailedRollout)
	if dec != DecisionDenied {
		t.Fatal("empty policy allow must deny")
	}
}

func TestProposeOnlyNeverApplied(t *testing.T) {
	audit := &MemAudit{}
	eng := &Engine{Policy: DefaultPolicy(), Audit: audit}
	ctx := ctxbuild.AgentContext{
		Namespace: "payments",
		Incident: incident.Incident{
			ID:              "inc-1",
			Summary:         "Deployment rollout failed ProgressDeadlineExceeded",
			Confidence:      0.9,
			PrimaryResource: &incident.ResourceRef{Kind: "Deployment", Name: "api"},
		},
		Deployment: &ctxbuild.DeploymentSnapshot{Name: "api", DesiredReplicas: 3, ReadyReplicas: 0},
		Target:     &incident.ResourceRef{Kind: "Deployment", Name: "api"},
	}
	p, err := eng.ProposeFromContext(ctx, 0.9)
	if err != nil || p == nil {
		t.Fatalf("%v %+v", err, p)
	}
	if p.Decision != DecisionProposed || p.Applied {
		t.Fatalf("expected proposed not applied: %+v", p)
	}
	if p.ActionID != ActionRollbackFailedRollout {
		t.Fatalf("action=%s", p.ActionID)
	}
	if len(audit.Entries) != 1 {
		t.Fatalf("audit=%d", len(audit.Entries))
	}
}

func TestConfidenceGate(t *testing.T) {
	eng := &Engine{Policy: DefaultPolicy(), Audit: &MemAudit{}}
	ctx := ctxbuild.AgentContext{
		Namespace: "payments",
		Incident: incident.Incident{
			Summary:         "rollout failed",
			PrimaryResource: &incident.ResourceRef{Kind: "Deployment", Name: "api"},
		},
		Deployment: &ctxbuild.DeploymentSnapshot{Name: "api", DesiredReplicas: 2, ReadyReplicas: 0},
	}
	p, err := eng.ProposeFromContext(ctx, 0.5)
	if err != nil || p == nil || p.Decision != DecisionDenied {
		t.Fatalf("%v %+v", err, p)
	}
}

func TestRestartNotInMVPAllowlistApply(t *testing.T) {
	// restart may exist as a string but is not in MVPAllowlist → hard deny
	dec, _ := EvaluateAction(Policy{Allow: []string{ActionRestartDeployment}}, ActionRestartDeployment)
	if dec != DecisionDenied {
		t.Fatal("restartDeployment must be hard-denied at MVP allowlist layer")
	}
}
