package coordinator

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kprompt/kprompt/internal/agent/handoff"
	"github.com/kprompt/kprompt/internal/incident"
)

func sampleReport(ns, summary string) incident.InvestigationReport {
	r := incident.NewInvestigationReport(ns, time.Now().UTC())
	r.Summary = summary
	r.Confidence = 0.8
	r.Hypotheses = []incident.Hypothesis{{Statement: "origin hyp", Primary: true, Confidence: 0.8}}
	r.Unknowns = []string{"dependency may be outside namespace"}
	return r
}

func TestMergeWithoutSuspectKeepsUnknown(t *testing.T) {
	origin := sampleReport("payments", "timeout to redis")
	got := Merge(origin, nil, "platform")
	if got.Confidence > 0.7 {
		t.Fatalf("confidence should be capped, got %v", got.Confidence)
	}
	blob := strings.Join(got.Unknowns, " ")
	if !strings.Contains(blob, "platform") {
		t.Fatalf("unknowns=%v", got.Unknowns)
	}
}

func TestMergeWithSuspect(t *testing.T) {
	origin := sampleReport("payments", "timeout")
	suspect := sampleReport("platform", "redis down")
	suspect.Evidence = []incident.EvidenceRef{{
		Type: incident.EvidenceEvent, Reason: "Unhealthy", Message: "redis not ready",
		Resource: &incident.ResourceRef{Kind: "Pod", Name: "redis-0"},
	}}
	got := Merge(origin, &suspect, "platform")
	if len(got.Evidence) == 0 {
		t.Fatal("expected suspect evidence")
	}
	if !strings.Contains(got.Summary, "redis down") {
		t.Fatalf("summary=%q", got.Summary)
	}
	if got.Confidence >= 0.8 {
		t.Fatalf("expected confidence penalty, got %v", got.Confidence)
	}
}

func TestHandleAndHTTP(t *testing.T) {
	svc := New()
	env := handoff.New("payments", "platform", "cross-ns dependency", sampleReport("payments", "timeout"))
	reply, err := svc.Handle(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Kind != KindReply || reply.MutateAttempted {
		t.Fatalf("%+v", reply)
	}
	if len(svc.Recent()) != 1 {
		t.Fatal("expected one record")
	}

	h := &Handler{Service: svc}
	raw, _ := json.Marshal(env)
	req := httptest.NewRequest(http.MethodPost, "/v1/handoff", bytes.NewReader(raw))
	rr := httptest.NewRecorder()
	h.routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got Reply
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.FromNamespace != "payments" {
		t.Fatalf("%+v", got)
	}
}
