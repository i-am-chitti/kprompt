package analyze

import (
	"context"
	"strings"
	"testing"

	"github.com/kprompt/kprompt/internal/agent/ctxbuild"
	"github.com/kprompt/kprompt/internal/agent/patterns"
	"github.com/kprompt/kprompt/internal/incident"
)

func TestPatternsBoostSeenBefore(t *testing.T) {
	lib := patterns.New(patterns.NewMemStore())
	a := New(nil, Options{HeuristicOnly: true, MinConfidence: 0.5})
	a.Patterns = lib

	agentCtx := ctxbuild.AgentContext{
		Namespace: "payments",
		Incident: incident.Incident{
			ID:              "a",
			Summary:         "CrashLoopBackOff on api",
			Severity:        incident.SeverityHigh,
			Evidence:        []incident.EvidenceRef{{Type: incident.EvidenceEvent, Reason: "BackOff", Message: "Back-off restarting failed container"}},
			PrimaryResource: &incident.ResourceRef{Kind: "Pod", Name: "api-x"},
		},
		Target: &incident.ResourceRef{Kind: "Pod", Name: "api-x"},
	}

	// Seed history (2 priors required).
	for i := 0; i < 2; i++ {
		agentCtx.Incident.ID = "seed-" + string(rune('a'+i))
		if _, err := a.Analyze(context.Background(), agentCtx, incident.AlertFired); err != nil {
			t.Fatal(err)
		}
	}
	agentCtx.Incident.ID = "live"
	out, err := a.Analyze(context.Background(), agentCtx, incident.AlertFired)
	if err != nil {
		t.Fatal(err)
	}
	if out.SeenBefore == "" || !strings.Contains(out.Result.RootCause, "Seen before") {
		t.Fatalf("expected seen-before boost: %+v", out)
	}
	if out.Result.Confidence < 0.8 {
		t.Fatalf("expected boosted confidence, got %v", out.Result.Confidence)
	}
}
