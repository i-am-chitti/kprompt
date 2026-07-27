package confidence

import (
	"testing"

	"github.com/kprompt/kprompt/internal/agent/ctxbuild"
	"github.com/kprompt/kprompt/internal/incident"
)

func TestAdjustNotEnoughEvidence(t *testing.T) {
	ctx := ctxbuild.AgentContext{Incident: incident.Incident{ID: "x", Namespace: "ns"}}
	got, note := Adjust(0.8, ctx, false, false)
	if got > 0.4 || note == "" {
		t.Fatalf("got=%v note=%q", got, note)
	}
}

func TestAdjustBoostWithDetectorAndEvidence(t *testing.T) {
	ctx := ctxbuild.AgentContext{
		Incident: incident.Incident{
			Evidence: []incident.EvidenceRef{{Type: incident.EvidenceEvent}, {Type: incident.EvidenceLog}},
		},
		RecentEvents: []incident.EvidenceRef{{Type: incident.EvidenceEvent}},
		LogSnippets:  []incident.EvidenceRef{{Type: incident.EvidenceLog}},
		Metrics:      []incident.EvidenceRef{{Type: incident.EvidenceMetric}},
	}
	got, _ := Adjust(0.8, ctx, true, false)
	if got < 0.8 {
		t.Fatalf("expected boost or hold, got %v", got)
	}
}

func TestAdjustDegradedPenalty(t *testing.T) {
	ctx := ctxbuild.AgentContext{
		Incident: incident.Incident{
			Evidence: []incident.EvidenceRef{{Type: incident.EvidenceEvent}, {Type: incident.EvidenceEvent}},
		},
		Degraded: []string{"prometheus", "otel"},
	}
	got, _ := Adjust(0.9, ctx, true, false)
	if got >= 0.9 {
		t.Fatalf("expected penalty, got %v", got)
	}
}

func TestAdjustLLMTrustedSkipsHarshCap(t *testing.T) {
	ctx := ctxbuild.AgentContext{
		Incident: incident.Incident{
			Evidence: []incident.EvidenceRef{{Type: incident.EvidenceEvent}},
		},
	}
	got, note := Adjust(0.94, ctx, false, true)
	if got < 0.9 {
		t.Fatalf("llm trusted should keep confidence, got %v note=%q", got, note)
	}
}
