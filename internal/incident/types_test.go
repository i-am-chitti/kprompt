package incident

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestIncidentAgentAlertRoundTrip(t *testing.T) {
	started := time.Date(2026, 7, 25, 9, 21, 0, 0, time.UTC)
	inc := NewIncident("inc-42", "payments", started)
	inc.Severity = SeverityCritical
	inc.Confidence = 0.94
	inc.Summary = "CrashLoopBackOff detected"
	inc.RootCause = "Redis connection timeout"
	inc.Recommendation = "Check redis-service endpoint"
	inc.PrimaryResource = &ResourceRef{
		APIVersion: "apps/v1",
		Kind:       "Deployment",
		Name:       "payment-api",
		Namespace:  "payments",
	}
	inc.Affected = []ResourceRef{*inc.PrimaryResource}
	inc.Evidence = []EvidenceRef{{
		Type:    EvidenceEvent,
		Reason:  "BackOff",
		Message: "Back-off restarting failed container",
		Source:  "kubernetes",
		Resource: &ResourceRef{
			Kind:      "Pod",
			Name:      "payment-api-75b89",
			Namespace: "payments",
		},
	}}

	if err := ValidateIncident(inc); err != nil {
		t.Fatalf("ValidateIncident: %v", err)
	}

	alert := NewAgentAlert(inc, AlertFired, started)
	if err := ValidateAgentAlert(alert); err != nil {
		t.Fatalf("ValidateAgentAlert: %v", err)
	}
	if alert.Kind != KindAgentAlert || alert.IncidentID != "inc-42" {
		t.Fatalf("unexpected alert: %+v", alert)
	}
	if !MeetsAlertGate(alert, DefaultMinSeverity(), 0.8) {
		t.Fatal("expected alert to meet gate")
	}
	if MeetsAlertGate(alert, SeverityCritical, 0.99) {
		t.Fatal("expected confidence gate to block")
	}

	raw, err := json.Marshal(alert)
	if err != nil {
		t.Fatal(err)
	}
	var decoded AgentAlert
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.RootCause != "Redis connection timeout" || decoded.Confidence != 0.94 {
		t.Fatalf("round-trip mismatch: %+v", decoded)
	}
}

func TestInvestigationValidate(t *testing.T) {
	inv := NewInvestigation("why is payment-api crashlooping?", "payments")
	inv.Summary = "Pod cannot resolve redis-service"
	inv.Confidence = 0.9
	inv.Findings = []Finding{{
		Code:     "dns.timeout",
		Severity: SeverityHigh,
		Title:    "Redis DNS timeout",
		Message:  "getaddrinfo failed for redis-service",
		Evidence: []EvidenceRef{{Type: EvidenceLog, Message: "dial tcp: lookup redis-service"}},
	}}
	inv.Degraded = []string{"prometheus"}
	if err := ValidateInvestigation(inv); err != nil {
		t.Fatal(err)
	}

	bad := inv
	bad.Summary = ""
	if err := ValidateInvestigation(bad); err == nil {
		t.Fatal("expected summary required")
	}
}

func TestValidateRejectsBadConfidence(t *testing.T) {
	inc := NewIncident("x", "ns", time.Now())
	inc.Confidence = 1.5
	if err := ValidateIncident(inc); err == nil {
		t.Fatal("expected confidence error")
	}
}

func TestInvestigationReportRoundTrip(t *testing.T) {
	at := time.Date(2026, 7, 27, 15, 0, 0, 0, time.UTC)
	rep := NewInvestigationReport("payments", at)
	rep.ID = "rep-1"
	rep.IncidentID = "inc-42"
	rep.Summary = "High latency after deploy"
	rep.Facts = "payment-api restart count rose; OOMKilled on payment-api-75b89"
	rep.Reasoning = "Restart coincides with OOM; memory grew after v1.4.2 rollout"
	rep.Confidence = 0.91
	rep.Severity = SeverityHigh
	rep.Evidence = []EvidenceRef{{
		Type:    EvidenceEvent,
		Reason:  "OOMKilled",
		Message: "Memory limit exceeded",
		Source:  "kubernetes",
	}}
	rep.Timeline = []EvidenceRef{
		{Type: EvidenceObject, Message: "Deployment payment-api rolled out v1.4.2", Timestamp: &at},
		{Type: EvidenceEvent, Reason: "OOMKilled", Message: "container killed"},
	}
	rep.Hypotheses = []Hypothesis{
		{
			Statement:   "Memory leak after deployment v1.4.2",
			CausalChain: []string{"High latency", "Pod restart", "OOMKilled", "Memory leak after deployment", "Deployment introduced regression"},
			Confidence:  0.91,
			Primary:     true,
		},
		{Statement: "Node memory pressure", Confidence: 0.35},
		{Statement: "Traffic spike", Confidence: 0.22},
	}
	rep.RecommendedActions = []RecommendedAction{{
		Title:          "Rollback payment-api to previous revision",
		Why:            "Regression correlates with last rollout",
		Risk:           "Brief traffic disruption during rollback",
		ExpectedImpact: "Restore prior memory profile",
		Rollback:       "Roll forward to v1.4.2 if rollback fails health checks",
		Confidence:     0.88,
		ActionID:       "rollbackFailedRollout",
	}}
	rep.Risks = []string{"Rollback may drop in-flight requests"}
	rep.Unknowns = []string{"prometheus metrics unavailable"}
	rep.Degraded = []string{"prometheus"}

	if err := ValidateInvestigationReport(rep); err != nil {
		t.Fatalf("ValidateInvestigationReport: %v", err)
	}
	primary := rep.PrimaryHypothesis()
	if primary == nil || primary.Statement != "Memory leak after deployment v1.4.2" {
		t.Fatalf("primary hypothesis: %+v", primary)
	}
	if len(primary.CausalChain) != 5 {
		t.Fatalf("causal chain len=%d", len(primary.CausalChain))
	}

	raw, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	var decoded InvestigationReport
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Kind != KindInvestigationReport || decoded.SchemaVersion != SchemaVersion2 {
		t.Fatalf("schema: kind=%s ver=%s", decoded.Kind, decoded.SchemaVersion)
	}
	if decoded.Confidence != 0.91 || len(decoded.Hypotheses) != 3 {
		t.Fatalf("round-trip mismatch: %+v", decoded)
	}
}

func TestReportFromIncident(t *testing.T) {
	inc := NewIncident("inc-7", "payments", time.Now())
	inc.Summary = "CrashLoop"
	inc.RootCause = "Bad image pull"
	inc.Recommendation = "Fix image tag"
	inc.Confidence = 0.8
	inc.Severity = SeverityHigh

	rep := ReportFromIncident(inc, time.Time{})
	if err := ValidateInvestigationReport(rep); err != nil {
		t.Fatal(err)
	}
	if rep.IncidentID != "inc-7" || rep.SchemaVersion != SchemaVersion2 {
		t.Fatalf("unexpected report: %+v", rep)
	}
	if p := rep.PrimaryHypothesis(); p == nil || p.Statement != "Bad image pull" {
		t.Fatalf("hypothesis: %+v", p)
	}
	if len(rep.RecommendedActions) != 1 || rep.RecommendedActions[0].Title != "Fix image tag" {
		t.Fatalf("actions: %+v", rep.RecommendedActions)
	}
}

func TestValidateInvestigationReportRejects(t *testing.T) {
	rep := NewInvestigationReport("ns", time.Now())
	rep.Summary = "x"
	rep.Confidence = 0.5
	if err := ValidateInvestigationReport(rep); err != nil {
		t.Fatal(err)
	}
	rep.Hypotheses = []Hypothesis{{Statement: "", Confidence: 0.1}}
	if err := ValidateInvestigationReport(rep); err == nil {
		t.Fatal("expected empty statement error")
	}
}

func TestFormatReportText(t *testing.T) {
	at := time.Date(2026, 7, 27, 15, 0, 0, 0, time.UTC)
	rep := NewInvestigationReport("payments", at)
	rep.Summary = "OOM after deploy"
	rep.Facts = "payment-api OOMKilled"
	rep.Reasoning = "detector=oom.killed"
	rep.Confidence = 0.91
	rep.Severity = SeverityCritical
	rep.Hypotheses = []Hypothesis{{
		Statement:   "Memory leak",
		CausalChain: []string{"latency", "restart", "OOM"},
		Confidence:  0.91,
		Primary:     true,
	}}
	rep.RecommendedActions = []RecommendedAction{{
		Title: "Rollback", Why: "regression", Risk: "brief blip", Confidence: 0.8,
	}}
	rep.Unknowns = []string{"no prometheus"}

	var buf strings.Builder
	if err := FormatReportText(&buf, rep); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"Facts", "Hypotheses", "Confidence", "Recommendations", "Unknowns", "latency → restart → OOM"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
