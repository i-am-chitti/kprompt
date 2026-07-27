package autopilot

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/kprompt/kprompt/internal/agent/ctxbuild"
	"github.com/kprompt/kprompt/internal/incident"
)

func TestHardDenyOutsideAllowlist(t *testing.T) {
	dec, reason := EvaluateAction(DefaultPolicy(), "deleteNamespace")
	if dec != DecisionDenied || !strings.Contains(reason, "hard-deny") {
		t.Fatalf("%s %s", dec, reason)
	}
}

func TestHardDenyPack(t *testing.T) {
	if denied, _ := HardDenyAction("wipeCluster"); !denied {
		t.Fatal("expected wipeCluster deny")
	}
	if denied, _ := HardDenyPlanText("wipe the cluster now", nil); !denied {
		t.Fatal("expected plan text deny")
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
	if p.Why == "" || p.Rollback == "" {
		t.Fatalf("expected AG-041 explain fields: %+v", p)
	}
	if len(audit.Entries) != 1 {
		t.Fatalf("audit=%d", len(audit.Entries))
	}
}

func TestDetectRestart(t *testing.T) {
	eng := &Engine{Policy: DefaultPolicy(), Audit: &MemAudit{}}
	ctx := ctxbuild.AgentContext{
		Namespace: "payments",
		Incident: incident.Incident{
			Summary:         "CrashLoopBackOff on api",
			PrimaryResource: &incident.ResourceRef{Kind: "Deployment", Name: "api"},
		},
		Deployment: &ctxbuild.DeploymentSnapshot{Name: "api", DesiredReplicas: 2, ReadyReplicas: 1},
	}
	p, err := eng.ProposeFromContext(ctx, 0.9)
	if err != nil || p == nil || p.ActionID != ActionRestartDeployment {
		t.Fatalf("%v %+v", err, p)
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

func TestApplyRequiresPolicyAuto(t *testing.T) {
	eng := &Engine{Policy: DefaultPolicy(), Audit: &MemAudit{}}
	prop := Proposal{
		ActionID: ActionRollbackFailedRollout, Namespace: "payments", TargetName: "api",
		Confidence: 0.9, Plan: PlanBody{Summary: "rollback api"},
	}
	_, err := eng.ApplyProposal(context.Background(), nil, prop)
	if err == nil || !strings.Contains(err.Error(), "policyAuto") {
		t.Fatalf("expected policyAuto gate, got %v", err)
	}
}

func TestParsePolicyMode(t *testing.T) {
	p, err := ParsePolicy([]byte(`{"allow":["restartDeployment"],"mode":"policyAuto","apply":true,"minConfidence":0.9}`))
	if err != nil {
		t.Fatal(err)
	}
	if !p.PolicyAuto() || p.MinConfidence != 0.9 {
		t.Fatalf("%+v", p)
	}
	p2, err := ParsePolicy([]byte(`{"allow":["restartDeployment"],"mode":"proposeOnly","apply":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if p2.Apply || p2.PolicyAuto() {
		t.Fatalf("proposeOnly must force Apply=false: %+v", p2)
	}
}

func TestLoadPolicyFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/policy.json"
	raw, _ := json.Marshal(Policy{Allow: []string{ActionEvictPod}, Mode: ModeProposeOnly})
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := LoadPolicyFile(path)
	if err != nil || !inList(p.Allow, ActionEvictPod) {
		t.Fatalf("%v %+v", err, p)
	}
}
