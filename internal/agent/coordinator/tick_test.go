package coordinator

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kprompt/kprompt/internal/agent/handoff"
	"github.com/kprompt/kprompt/internal/incident"
)

type countingProbe struct {
	n int
}

func (c *countingProbe) Probe(ctx context.Context, suspect string, env handoff.Envelope) (*incident.InvestigationReport, error) {
	c.n++
	return &incident.InvestigationReport{
		APIVersion:    incident.APIVersion,
		Kind:          incident.KindInvestigationReport,
		SchemaVersion: incident.SchemaVersion2,
		Namespace:     suspect,
		Summary:       "probe ok",
		Reasoning:     probeSourceKube,
		Evidence: []incident.EvidenceRef{{
			Type:    incident.EvidenceEvent,
			Source:  probeSourceKube,
			Message: "probed",
		}},
		CreatedAt: time.Now().UTC(),
	}, nil
}

func TestTickProactiveRefresh(t *testing.T) {
	svc := New()
	probe := &countingProbe{}
	svc.Probe = probe
	env := handoff.Envelope{
		APIVersion:       handoff.APIVersion,
		Kind:             handoff.Kind,
		SchemaVersion:    handoff.SchemaVersion,
		FromNamespace:    "payments",
		SuspectNamespace: "platform",
		Reason:           "dependency",
		CreatedAt:        time.Now().UTC(),
		Report: incident.InvestigationReport{
			APIVersion:    incident.APIVersion,
			Kind:          incident.KindInvestigationReport,
			SchemaVersion: incident.SchemaVersion2,
			Namespace:     "payments",
			Summary:       "payments degraded",
			CreatedAt:     time.Now().UTC(),
		},
	}
	if _, err := svc.Handle(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	before := len(svc.Recent())
	res := svc.Tick(context.Background(), TickConfig{Budget: 3})
	if res.MutateAttempted {
		t.Fatal("mutate must stay false")
	}
	if res.Probed < 1 || res.Merged < 1 {
		t.Fatalf("%+v", res)
	}
	if probe.n < 2 { // initial handle + tick
		t.Fatalf("probe calls=%d", probe.n)
	}
	if len(svc.Recent()) <= before {
		t.Fatalf("expected tick to append record")
	}
	br := svc.BlastRadius("payments")
	if len(br.Hops) == 0 {
		t.Fatalf("blast empty: %+v", br)
	}
	aud := svc.Audit()
	var sawTick bool
	for _, a := range aud {
		if a.Kind == reasonProactiveTick && !a.MutateAttempted {
			sawTick = true
		}
	}
	if !sawTick {
		t.Fatalf("audit=%+v", aud)
	}
}

func TestFilterHopsByDistance(t *testing.T) {
	hops := []BlastRadiusHop{
		{From: "a", To: "b", Count: 1},
		{From: "b", To: "c", Count: 1},
		{From: "c", To: "d", Count: 1},
	}
	got := filterHopsByDistance(hops, "a", 1)
	if len(got) != 1 || got[0].To != "b" {
		t.Fatalf("%+v", got)
	}
	got2 := filterHopsByDistance(hops, "a", 2)
	if len(got2) != 2 {
		t.Fatalf("%+v", got2)
	}
}

func TestBlastRadiusMeshStatus(t *testing.T) {
	recs := []Record{{
		Envelope: handoff.Envelope{FromNamespace: "a", SuspectNamespace: "b"},
		At:       time.Now().UTC(),
	}}
	deg := BlastRadius(recs, false, "", 3, false)
	if deg.Status != "degraded" || !strings.Contains(deg.Note, "degraded") {
		t.Fatalf("%+v", deg)
	}
	ok := BlastRadius(recs, false, "", 3, true)
	if ok.Status != "ok" {
		t.Fatalf("%+v", ok)
	}
}
