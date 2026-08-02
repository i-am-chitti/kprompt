package watchassist

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestWatchFindsCrashLoop(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	client := fake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "api-0", Namespace: "payments"},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{
					Name: "api",
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff", Message: "back-off"},
					},
					RestartCount: 6,
				}},
			},
		},
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "ev1", Namespace: "payments"},
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "api-0", Namespace: "payments"},
			Type:           corev1.EventTypeWarning,
			Reason:         "Unhealthy",
			Message:        "liveness probe failed",
			LastTimestamp:  metav1.NewTime(now.Add(-2 * time.Minute)),
		},
	)
	got, err := (&Analyzer{Client: client}).Run(context.Background(), Request{
		Namespace: "payments",
		Now:       now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Suggestions) == 0 {
		t.Fatal("expected suggestions")
	}
	found := false
	for _, s := range got.Suggestions {
		if s.Code == "Watch.PodWaiting.CrashLoopBackOff" {
			found = true
			if s.Command == "" {
				t.Fatal("expected investigate command")
			}
		}
	}
	if !found {
		t.Fatalf("missing crashloop: %+v", got.Suggestions)
	}
}

func TestWatchRequiresNamespace(t *testing.T) {
	_, err := (&Analyzer{Client: fake.NewSimpleClientset()}).Run(context.Background(), Request{})
	if err == nil {
		t.Fatal("expected namespace error")
	}
}
