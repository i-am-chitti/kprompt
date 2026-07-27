package detect

import (
	"strings"
	"testing"
	"time"

	"github.com/kprompt/kprompt/internal/agent/ctxbuild"
	"github.com/kprompt/kprompt/internal/incident"
)

func TestCatalogOOM(t *testing.T) {
	hit, ok := Catalog(ctxWithEvidence("OOMKilled", "Memory limit exceeded"))
	if !ok || hit.Code != "oom.killed" {
		t.Fatalf("hit=%+v ok=%v", hit, ok)
	}
	if len(hit.CausalChain) < 3 {
		t.Fatalf("causal chain: %v", hit.CausalChain)
	}
}

func TestCatalogImagePullBeatsCrashLoop(t *testing.T) {
	hit, ok := Catalog(ctxWithEvidence("BackOff", `Back-off pulling image "x:9": ImagePullBackOff`))
	if !ok || hit.Code != "image.pull" {
		t.Fatalf("hit=%+v ok=%v", hit, ok)
	}
}

func TestCatalogCrashLoop(t *testing.T) {
	hit, ok := Catalog(ctxWithEvidence("BackOff", "CrashLoopBackOff"))
	if !ok || hit.Code != "crashloop" {
		t.Fatalf("hit=%+v ok=%v", hit, ok)
	}
}

func TestCatalogPending(t *testing.T) {
	hit, ok := Catalog(ctxWithEvidence("FailedScheduling", "0/3 nodes available"))
	if !ok || hit.Code != "schedule.pending" {
		t.Fatalf("hit=%+v ok=%v", hit, ok)
	}
}

func TestCatalogDNS(t *testing.T) {
	hit, ok := Catalog(ctxWithEvidence("", "dial tcp: lookup redis-service: no such host"))
	if !ok || hit.Code != "dns.fail" {
		t.Fatalf("hit=%+v ok=%v", hit, ok)
	}
	if !strings.Contains(hit.RootCause, "DNS") {
		t.Fatalf("root: %s", hit.RootCause)
	}
}

func TestCatalogNoMatch(t *testing.T) {
	ctx := ctxbuild.AgentContext{
		Incident: incident.Incident{
			ID:        "i",
			Namespace: "ns",
			Summary:   "all green",
		},
	}
	if _, ok := Catalog(ctx); ok {
		t.Fatal("expected no match")
	}
}

func ctxWithEvidence(reason, message string) ctxbuild.AgentContext {
	inc := incident.NewIncident("inc-1", "payments", time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC))
	inc.Summary = message
	inc.Evidence = []incident.EvidenceRef{{
		Type:    incident.EvidenceEvent,
		Reason:  reason,
		Message: message,
		Source:  "kubernetes",
	}}
	return ctxbuild.AgentContext{Incident: inc, Namespace: "payments"}
}
