package correlate

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/watch"

	agentwatch "github.com/kprompt/kprompt/internal/agent/watch"
	"github.com/kprompt/kprompt/internal/incident"
)

func TestPodWorkloadName(t *testing.T) {
	if got := podWorkloadName("payment-api-7d9f8c5b6d-xk2pq"); got != "payment-api" {
		t.Fatalf("got %q", got)
	}
	if got := podWorkloadName("redis"); got != "redis" {
		t.Fatalf("got %q", got)
	}
}

func TestCorrelateMergeAndDedupe(t *testing.T) {
	now := time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC)
	clock := now
	b := NewBuilder(Options{
		Namespace: "payments",
		Window:    5 * time.Minute,
		DedupeTTL: time.Minute,
		Now:       func() time.Time { return clock },
	})

	ev1 := agentwatch.Event{
		Type:         watch.Added,
		Resource:     agentwatch.ResourceEvent,
		Namespace:    "payments",
		Name:         "pod.1",
		Reason:       "BackOff",
		Message:      "Back-off restarting failed container",
		InvolvedKind: "Pod",
		InvolvedName: "payment-api-7d9f8c5b6d-xk2pq",
		At:           clock,
	}
	ch, ok := b.Ingest(ev1)
	if !ok || ch.Kind != ChangeOpened {
		t.Fatalf("open: %+v ok=%v", ch, ok)
	}
	if ch.Incident.PrimaryResource == nil || ch.Incident.PrimaryResource.Name != "payment-api" {
		t.Fatalf("primary: %+v", ch.Incident.PrimaryResource)
	}

	clock = clock.Add(10 * time.Second)
	ev2 := ev1
	ev2.Name = "pod.2"
	ev2.At = clock
	ch, ok = b.Ingest(ev2)
	if ok {
		t.Fatalf("expected dedupe ignore, got %+v", ch)
	}

	clock = clock.Add(2 * time.Minute)
	ev3 := ev1
	ev3.Name = "pod.3"
	ev3.Message = "Back-off restarting failed container x2"
	ev3.At = clock
	ch, ok = b.Ingest(ev3)
	if !ok || ch.Kind != ChangeUpdated {
		t.Fatalf("update: %+v", ch)
	}
	if len(ch.Incident.Evidence) < 2 {
		t.Fatalf("expected merged evidence, got %d", len(ch.Incident.Evidence))
	}
	if len(b.OpenIncidents()) != 1 {
		t.Fatalf("expected one open incident")
	}
}

func TestRecoveryCloses(t *testing.T) {
	clock := time.Date(2026, 7, 25, 11, 0, 0, 0, time.UTC)
	b := NewBuilder(Options{
		Namespace: "payments",
		Now:       func() time.Time { return clock },
	})
	_, _ = b.Ingest(agentwatch.Event{
		Type: watch.Added, Resource: agentwatch.ResourceEvent, Namespace: "payments",
		Reason: "BackOff", InvolvedKind: "Pod", InvolvedName: "api-abcde-fghij", At: clock,
	})
	clock = clock.Add(time.Minute)
	ch, ok := b.Ingest(agentwatch.Event{
		Type: watch.Modified, Resource: agentwatch.ResourcePod, Namespace: "payments",
		Name: "api-abcde-fghij", PodPhase: "Running", At: clock,
	})
	if !ok || ch.Kind != ChangeClosed {
		t.Fatalf("recovery: %+v", ch)
	}
	if ch.Incident.Status != incident.StatusClosed {
		t.Fatalf("status %s", ch.Incident.Status)
	}
	if len(b.OpenIncidents()) != 0 {
		t.Fatal("expected no open")
	}
}

func TestReopenNewID(t *testing.T) {
	clock := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	b := NewBuilder(Options{
		Namespace:    "ns",
		ReopenWithin: 30 * time.Minute,
		Now:          func() time.Time { return clock },
	})
	ch1, _ := b.Ingest(agentwatch.Event{
		Type: watch.Added, Resource: agentwatch.ResourceEvent, Namespace: "ns",
		Reason: "Failed", InvolvedKind: "Pod", InvolvedName: "web-aaaaa-bbbbb", At: clock,
	})
	clock = clock.Add(time.Minute)
	_, _ = b.Ingest(agentwatch.Event{
		Type: watch.Modified, Resource: agentwatch.ResourcePod, Namespace: "ns",
		Name: "web-aaaaa-bbbbb", PodPhase: "Running", At: clock,
	})
	clock = clock.Add(2 * time.Minute)
	ch2, ok := b.Ingest(agentwatch.Event{
		Type: watch.Added, Resource: agentwatch.ResourceEvent, Namespace: "ns",
		Reason: "BackOff", InvolvedKind: "Pod", InvolvedName: "web-aaaaa-bbbbb", At: clock,
	})
	if !ok || ch2.Kind != ChangeReopened {
		t.Fatalf("reopen: %+v", ch2)
	}
	if ch2.Incident.ID == ch1.Incident.ID {
		t.Fatal("reopen must allocate a new incident id")
	}
}

func TestSweepIdleClose(t *testing.T) {
	clock := time.Date(2026, 7, 25, 13, 0, 0, 0, time.UTC)
	b := NewBuilder(Options{
		Namespace: "ns",
		QuietFor:  5 * time.Minute,
		Now:       func() time.Time { return clock },
	})
	_, _ = b.Ingest(agentwatch.Event{
		Type: watch.Added, Resource: agentwatch.ResourceEvent, Namespace: "ns",
		Reason: "Unhealthy", InvolvedKind: "Pod", InvolvedName: "db-aaaaa-bbbbb", At: clock,
	})
	clock = clock.Add(6 * time.Minute)
	changes := b.Sweep()
	if len(changes) != 1 || changes[0].Kind != ChangeClosed {
		t.Fatalf("sweep: %+v", changes)
	}
}
