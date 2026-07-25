package incident

import (
	"encoding/json"
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
