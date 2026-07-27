package correlate

import (
	"testing"
	"time"

	agentwatch "github.com/kprompt/kprompt/internal/agent/watch"
	"github.com/kprompt/kprompt/internal/incident"
	"k8s.io/apimachinery/pkg/watch"
)

func TestExportRestoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := FileStore{Dir: dir}
	b := NewBuilder(Options{Namespace: "payments"})
	b.SetStore(store)

	ev := agentwatch.Event{
		Type:     watch.Added,
		Resource: agentwatch.ResourceEvent,
		Namespace: "payments",
		Reason:   "BackOff",
		Message:  "CrashLoopBackOff",
		InvolvedKind: "Pod",
		InvolvedName: "api-abc-12345",
		At:       time.Now().UTC(),
	}
	ch, ok := b.Ingest(ev)
	if !ok || ch.Kind != ChangeOpened {
		t.Fatalf("ingest: %+v ok=%v", ch, ok)
	}
	_ = b.SetNotifierThread(ch.Incident.ID, "1234.5678")
	if err := b.Persist(); err != nil {
		t.Fatal(err)
	}

	b2 := NewBuilder(Options{Namespace: "payments"})
	b2.SetStore(store)
	snap, err := store.Load("payments")
	if err != nil {
		t.Fatal(err)
	}
	if err := b2.Restore(snap); err != nil {
		t.Fatal(err)
	}
	open := b2.OpenIncidents()
	if len(open) != 1 {
		t.Fatalf("open=%d", len(open))
	}
	if open[0].NotifierThread != "1234.5678" {
		t.Fatalf("thread=%q", open[0].NotifierThread)
	}
	if open[0].ID != ch.Incident.ID {
		t.Fatalf("id mismatch %s vs %s", open[0].ID, ch.Incident.ID)
	}
	if open[0].Status != incident.StatusOpen {
		t.Fatalf("status=%s", open[0].Status)
	}
}
