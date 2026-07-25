package health

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kprompt/kprompt/internal/incident"
)

func TestScoreDropsOnCriticalIncident(t *testing.T) {
	tr := NewTracker("payments", nil)
	tr.DropThreshold = 10
	s1 := tr.Evaluate(context.Background(), nil)
	if s1.Score != 100 || s1.Trend != "stable" {
		t.Fatalf("%+v", s1)
	}
	open := []incident.Incident{{
		ID: "inc-1", Namespace: "payments", Severity: incident.SeverityCritical, Status: incident.StatusOpen,
	}}
	s2 := tr.Evaluate(context.Background(), open)
	if s2.Score >= s1.Score {
		t.Fatalf("expected drop: %d → %d", s1.Score, s2.Score)
	}
	if s2.Trend != "risk_increasing" {
		t.Fatalf("trend=%s msg=%s", s2.Trend, s2.Message)
	}
	if !contains(s2.Degraded, "pods") {
		t.Fatalf("expected pods degraded without client: %v", s2.Degraded)
	}
}

func TestScoreUsesPodStats(t *testing.T) {
	podOK := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "ok", Namespace: "ns"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{{
				Type: corev1.PodReady, Status: corev1.ConditionTrue,
			}},
		},
	}
	podBad := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "bad", Namespace: "ns"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{{
				Type: corev1.PodReady, Status: corev1.ConditionFalse,
			}},
			ContainerStatuses: []corev1.ContainerStatus{{RestartCount: 12}},
		},
	}
	client := fake.NewSimpleClientset(podOK, podBad)
	tr := NewTracker("ns", client)
	clock := time.Date(2026, 7, 25, 15, 0, 0, 0, time.UTC)
	tr.Now = func() time.Time { return clock }

	s := tr.Evaluate(context.Background(), nil)
	if s.PodReady != "1/2" {
		t.Fatalf("ready=%s", s.PodReady)
	}
	if s.Restarts != 12 {
		t.Fatalf("restarts=%d", s.Restarts)
	}
	if s.Score >= 100 {
		t.Fatalf("expected pod penalty, score=%d", s.Score)
	}
}

func TestImprovingTrend(t *testing.T) {
	tr := NewTracker("ns", nil)
	tr.DropThreshold = 5
	_ = tr.Evaluate(context.Background(), []incident.Incident{{
		Severity: incident.SeverityHigh, Status: incident.StatusOpen,
	}})
	s := tr.Evaluate(context.Background(), nil)
	if s.Trend != "improving" {
		t.Fatalf("trend=%s", s.Trend)
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
