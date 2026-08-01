package roast

import (
	"strings"
	"testing"

	"github.com/kprompt/kprompt/internal/agent/health"
)

func TestFromSnapshotBands(t *testing.T) {
	r := FromSnapshot(health.Snapshot{Namespace: "payments", Score: 95, Trend: "stable", PodReady: "3/3"})
	if r.Verdict != "thriving" || r.Scope != ScopeNamespace {
		t.Fatalf("%+v", r)
	}
	if !strings.Contains(Format(r), "Score: 95/100") {
		t.Fatalf("format:\n%s", Format(r))
	}

	r = FromSnapshot(health.Snapshot{Namespace: "payments", Score: 12, Trend: "risk_increasing", Restarts: 9, OpenIncidents: 2})
	if r.Verdict != "on_fire" {
		t.Fatalf("verdict=%s", r.Verdict)
	}
	out := Format(r)
	if !strings.Contains(out, "risk_increasing") {
		t.Fatalf("expected trend note:\n%s", out)
	}
}

func TestFromFleetSortsWorstFirst(t *testing.T) {
	r := FromFleet([]health.Snapshot{
		{Namespace: "ok", Score: 90, Trend: "stable"},
		{Namespace: "bad", Score: 20, Trend: "risk_increasing"},
		{Namespace: "mid", Score: 55, Trend: "stable"},
	})
	if r.Scope != ScopeCluster || r.Score != 20 {
		t.Fatalf("%+v", r)
	}
	if len(r.Namespaces) != 3 || r.Namespaces[0].Namespace != "bad" {
		t.Fatalf("order=%+v", r.Namespaces)
	}
	out := Format(r)
	if !strings.Contains(out, "bad") || !strings.Contains(out, "Worst score: 20/100") {
		t.Fatalf("format:\n%s", out)
	}
}

func TestHeadlineStable(t *testing.T) {
	a := pickHeadline("payments", 42, "stable", 0, 0)
	b := pickHeadline("payments", 42, "stable", 0, 0)
	if a != b {
		t.Fatalf("%q vs %q", a, b)
	}
}
