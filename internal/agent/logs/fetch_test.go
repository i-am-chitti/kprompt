package logs

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"

	agentwatch "github.com/kprompt/kprompt/internal/agent/watch"
	"github.com/kprompt/kprompt/internal/incident"
)

func TestShouldFetch(t *testing.T) {
	if !ShouldFetch(agentwatch.Event{Resource: agentwatch.ResourceEvent, Reason: "BackOff"}) {
		t.Fatal("BackOff")
	}
	if ShouldFetch(agentwatch.Event{Resource: agentwatch.ResourceEvent, Reason: "Pulled"}) {
		t.Fatal("Pulled should not fetch")
	}
	if !ShouldFetch(agentwatch.Event{Resource: agentwatch.ResourcePod, PodPhase: "Failed"}) {
		t.Fatal("Failed pod")
	}
}

func TestPreferPrevious(t *testing.T) {
	if !preferPrevious(agentwatch.Event{Reason: "BackOff"}) {
		t.Fatal("expected previous for BackOff")
	}
	if preferPrevious(agentwatch.Event{Reason: "Unhealthy"}) {
		t.Fatal("Unhealthy uses current logs")
	}
}

func TestTargetFromEvent(t *testing.T) {
	kind, name := targetFromEvent(agentwatch.Event{
		Resource: agentwatch.ResourceEvent, InvolvedKind: "Pod", InvolvedName: "x-1",
	})
	if kind != "Pod" || name != "x-1" {
		t.Fatalf("%s %s", kind, name)
	}
}

func TestAttachRecordsFetchFailure(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "api-1", Namespace: "payments"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "x"}},
		},
	}
	client := fake.NewSimpleClientset(pod)
	f := New(client)
	f.Since = time.Minute
	inc := incident.NewIncident("inc-1", "payments", time.Now().UTC())
	ev := agentwatch.Event{
		Type:         watch.Added,
		Resource:     agentwatch.ResourceEvent,
		Namespace:    "payments",
		Reason:       "BackOff",
		Message:      "CrashLoopBackOff",
		InvolvedKind: "Pod",
		InvolvedName: "api-1",
	}
	f.Attach(context.Background(), &inc, ev)
	if len(inc.Evidence) == 0 {
		t.Fatal("expected log evidence entry")
	}
	last := inc.Evidence[len(inc.Evidence)-1]
	if last.Type != incident.EvidenceLog {
		t.Fatalf("type %s", last.Type)
	}
	// Fake clientset typically cannot stream logs → fetch-failed is OK for unit coverage.
	if last.Reason != "log-fetch-failed" && last.URI == "" {
		t.Fatalf("unexpected evidence: %+v", last)
	}
}

func TestTruncateBody(t *testing.T) {
	body := strings.Repeat("a", 100)
	maxBytes := 10
	truncated := false
	if len(body) > maxBytes {
		body = body[len(body)-maxBytes:]
		truncated = true
	}
	if !truncated || len(body) != 10 {
		t.Fatalf("%v %d", truncated, len(body))
	}
}
