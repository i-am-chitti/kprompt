package priority

import (
	"strings"
	"testing"

	"github.com/kprompt/kprompt/internal/agent/ctxbuild"
	"github.com/kprompt/kprompt/internal/incident"
)

func TestClassifyOOMIsOutage(t *testing.T) {
	c := Classify(ctxbuild.AgentContext{}, "oom.killed", incident.SeverityCritical, "OOM", "memory")
	if c.Objective != ObjectiveOutage || c.Rank != 1 {
		t.Fatalf("%+v", c)
	}
}

func TestClassifyCrashloopAvailability(t *testing.T) {
	c := Classify(ctxbuild.AgentContext{}, "crashloop", incident.SeverityHigh, "CrashLoop", "")
	if c.Objective != ObjectiveAvailability || c.Rank != 4 {
		t.Fatalf("%+v", c)
	}
}

func TestApplySeverityNeverLowers(t *testing.T) {
	got := ApplySeverity(incident.SeverityCritical, ObjectiveBestPractices)
	if got != incident.SeverityCritical {
		t.Fatalf("got %s", got)
	}
	got = ApplySeverity(incident.SeverityInfo, ObjectiveOutage)
	if got != incident.SeverityCritical {
		t.Fatalf("floor outage → critical, got %s", got)
	}
}

func TestSortActions(t *testing.T) {
	actions := []incident.RecommendedAction{
		{Title: "rightsize CPU for cost"},
		{Title: "mitigate production outage now"},
	}
	SortActions(actions)
	if !strings.Contains(actions[0].Title, "outage") {
		t.Fatalf("expected outage first, got %+v", actions)
	}
}
