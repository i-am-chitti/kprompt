package handoff

import (
	"strings"
	"testing"
	"time"

	"github.com/kprompt/kprompt/internal/incident"
)

func sampleReport(ns string) incident.InvestigationReport {
	r := incident.NewInvestigationReport(ns, time.Now().UTC())
	r.Summary = "CrashLoop on api"
	r.Confidence = 0.7
	r.Hypotheses = []incident.Hypothesis{{Statement: "OOM", Primary: true, Confidence: 0.7}}
	return r
}

func TestValidateAndNew(t *testing.T) {
	rep := sampleReport("payments")
	env := New("payments", "platform", "dependency may be outside my namespace", rep)
	if err := Validate(env); err != nil {
		t.Fatal(err)
	}
	if env.Kind != Kind || env.SchemaVersion != SchemaVersion {
		t.Fatalf("%+v", env)
	}
}

func TestValidateRejectsEmpty(t *testing.T) {
	if err := Validate(Envelope{Kind: Kind}); err == nil {
		t.Fatal("expected error")
	}
}

func TestNeedsHandoff(t *testing.T) {
	rep := sampleReport("payments")
	rep.Unknowns = []string{"dependency may be outside namespace"}
	_, reason, ok := NeedsHandoff("payments", rep)
	if !ok || !strings.Contains(reason, "Coordinator") {
		t.Fatalf("ok=%v reason=%q", ok, reason)
	}
	rep2 := sampleReport("payments")
	if _, _, ok := NeedsHandoff("payments", rep2); ok {
		t.Fatal("expected no handoff")
	}
}
