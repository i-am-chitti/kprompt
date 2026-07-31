package brief

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kprompt/kprompt/internal/agent/correlate"
	"github.com/kprompt/kprompt/internal/agent/memory"
	"github.com/kprompt/kprompt/internal/agent/patterns"
	"github.com/kprompt/kprompt/internal/incident"
	"k8s.io/client-go/kubernetes/fake"
)

func TestBuildBrief(t *testing.T) {
	dir := t.TempDir()
	incStore := correlate.FileStore{Dir: dir + "/inc"}
	_ = incStore.Save(correlate.Snapshot{
		Namespace: "payments",
		Open: map[string]incident.Incident{
			"inc-1": {ID: "inc-1", Namespace: "payments", Summary: "crash", Severity: incident.SeverityHigh},
		},
		UpdatedAt: time.Now().UTC(),
	})
	patStore := patterns.FileStore{Dir: dir + "/pat"}
	_ = patStore.Save(patterns.Snapshot{
		Namespace: "payments",
		Patterns: []patterns.Pattern{{
			ID: "p1", Signature: "crashloop", Namespace: "payments", Count: 3,
		}},
	})
	memStore := memory.FileStore{Dir: dir + "/mem"}
	_, _ = memory.New(memStore).Upsert("payments", memory.Fact{
		Kind: memory.KindDependency, Key: "redis", Source: "discover",
	})

	b, err := Build(context.Background(), "payments", Inputs{
		Client:    fake.NewSimpleClientset(),
		Incidents: incStore,
		Patterns:  patStore,
		Memory:    memStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	if b.Kind != kindBrief || b.OpenIncidents != 1 || b.Patterns != 1 || b.MemoryDeps != 1 {
		t.Fatalf("%+v", b)
	}
	if b.Health == nil || b.Health.Score > 100 {
		t.Fatalf("health=%+v", b.Health)
	}
	text := Format(b)
	if !strings.Contains(text, "payments") || !strings.Contains(text, "redis") {
		t.Fatalf("format=%q", text)
	}
}
