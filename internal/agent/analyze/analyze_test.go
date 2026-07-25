package analyze

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/kprompt/kprompt/internal/agent/ctxbuild"
	"github.com/kprompt/kprompt/internal/incident"
	"github.com/kprompt/kprompt/internal/llm"
)

func TestHeuristicCrashLoop(t *testing.T) {
	inc := incident.NewIncident("inc-1", "payments", time.Now().UTC())
	inc.Summary = "BackOff on payment-api"
	inc.Evidence = []incident.EvidenceRef{{
		Type: incident.EvidenceEvent, Reason: "BackOff", Message: "CrashLoopBackOff",
	}}
	res := Heuristic(ctxbuild.AgentContext{Incident: inc, Namespace: "payments"})
	if res.Severity != incident.SeverityHigh || res.Confidence < 0.7 {
		t.Fatalf("%+v", res)
	}
}

func TestAnalyzeGateAndDedupe(t *testing.T) {
	stub := &llm.Stub{Structured: mustJSON(Result{
		Severity:       incident.SeverityCritical,
		Confidence:     0.94,
		Summary:        "CrashLoopBackOff",
		RootCause:      "Redis DNS timeout",
		Recommendation: "Check redis-service Endpoint",
	})}
	a := New(stub, Options{MinSeverity: incident.SeverityMedium, MinConfidence: 0.8})

	inc := incident.NewIncident("inc-42", "payments", time.Now().UTC())
	inc.Summary = "BackOff"
	inc.Affected = []incident.ResourceRef{{Kind: "Deployment", Name: "payment-api", Namespace: "payments"}}
	inc.Evidence = []incident.EvidenceRef{{Type: incident.EvidenceEvent, Reason: "BackOff", Message: "x"}}
	agentCtx := ctxbuild.AgentContext{
		APIVersion: ctxbuild.APIVersion,
		Kind:       ctxbuild.Kind,
		Namespace:  "payments",
		Incident:   inc,
	}

	out, err := a.Analyze(context.Background(), agentCtx, incident.AlertFired)
	if err != nil {
		t.Fatal(err)
	}
	if out.Skipped || !out.PassedGate || out.Source != "llm" {
		t.Fatalf("%+v", out)
	}
	if out.Alert.RootCause != "Redis DNS timeout" {
		t.Fatalf("alert: %+v", out.Alert)
	}
	if err := incident.ValidateAgentAlert(out.Alert); err != nil {
		t.Fatal(err)
	}

	out2, err := a.Analyze(context.Background(), agentCtx, incident.AlertUpdated)
	if err != nil {
		t.Fatal(err)
	}
	if !out2.Skipped {
		t.Fatal("expected dedupe skip on same evidence fingerprint")
	}
}

func TestAnalyzeFailsGate(t *testing.T) {
	a := New(nil, Options{
		HeuristicOnly: true,
		MinSeverity:   incident.SeverityCritical,
		MinConfidence: 0.99,
	})
	inc := incident.NewIncident("inc-3", "ns", time.Now().UTC())
	inc.Summary = "something odd"
	out, err := a.Analyze(context.Background(), ctxbuild.AgentContext{Incident: inc, Namespace: "ns"}, incident.AlertFired)
	if err != nil {
		t.Fatal(err)
	}
	if out.PassedGate {
		t.Fatalf("expected gate fail: %+v", out)
	}
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
