package timeline

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kprompt/kprompt/internal/incident"
)

func TestBuilderDeploymentTimeline(t *testing.T) {
	ns := "payments"
	now := time.Now().UTC()
	min := int32(1)
	client := fake.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: "api", Namespace: ns, UID: types.UID("dep1"),
				CreationTimestamp: metav1.NewTime(now.Add(-2 * time.Hour)),
			},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
			},
		},
		&appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "api-rs1", Namespace: ns,
				CreationTimestamp: metav1.NewTime(now.Add(-30 * time.Minute)),
				Annotations:       map[string]string{"deployment.kubernetes.io/revision": "2"},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "apps/v1", Kind: "Deployment", Name: "api", UID: "dep1",
				}},
				Labels: map[string]string{"app": "api"},
			},
			Status: appsv1.ReplicaSetStatus{Replicas: 1, ReadyReplicas: 0},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "api-pod", Namespace: ns, Labels: map[string]string{"app": "api"},
			},
		},
		&corev1.Event{
			ObjectMeta: metav1.ObjectMeta{Name: "ev1", Namespace: ns},
			InvolvedObject: corev1.ObjectReference{
				Kind: "Pod", Name: "api-pod", Namespace: ns,
			},
			Reason:        "BackOff",
			Message:       "Back-off restarting failed container",
			LastTimestamp: metav1.NewTime(now.Add(-5 * time.Minute)),
			Type:          "Warning",
		},
		&autoscalingv2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name: "api-hpa", Namespace: ns,
				CreationTimestamp: metav1.NewTime(now.Add(-40 * time.Minute)),
			},
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
					Kind: "Deployment", Name: "api",
				},
				MinReplicas: &min,
				MaxReplicas: 5,
			},
			Status: autoscalingv2.HorizontalPodAutoscalerStatus{
				CurrentReplicas: 1, DesiredReplicas: 2,
			},
		},
	)

	doc, err := (&Builder{Client: client}).Run(context.Background(), Request{
		Name: "api", Namespace: ns, Kind: "Deployment",
		Prompt: "timeline for api", Window: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Timeline) == 0 {
		t.Fatal("expected timeline entries")
	}
	if !hasCode(doc.Findings, "Timeline.Rollouts") {
		t.Fatalf("missing rollouts finding: %+v", doc.Findings)
	}
	if !hasCode(doc.Findings, "Timeline.HPA") {
		t.Fatalf("missing HPA finding: %+v", doc.Findings)
	}
	if !hasCode(doc.Findings, "Timeline.Events") {
		t.Fatalf("missing events finding: %+v", doc.Findings)
	}
	for i := 1; i < len(doc.Timeline); i++ {
		a, b := doc.Timeline[i-1].Timestamp, doc.Timeline[i].Timestamp
		if a != nil && b != nil && a.After(*b) {
			t.Fatalf("timeline not sorted at %d: %v > %v", i, a, b)
		}
	}
	for _, d := range []string{"prometheus", "otel", "mesh"} {
		found := false
		for _, x := range doc.Degraded {
			if x == d {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected degraded %q", d)
		}
	}
}

func TestBuilderPodTimeline(t *testing.T) {
	ns := "payments"
	now := time.Now().UTC()
	client := fake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ledger", Namespace: ns,
				CreationTimestamp: metav1.NewTime(now.Add(-10 * time.Minute)),
			},
			Status: corev1.PodStatus{Phase: corev1.PodPending},
		},
		&corev1.Event{
			ObjectMeta: metav1.ObjectMeta{Name: "ev1", Namespace: ns},
			InvolvedObject: corev1.ObjectReference{
				Kind: "Pod", Name: "ledger", Namespace: ns,
			},
			Reason:        "FailedScheduling",
			Message:       "persistentvolumeclaim not found",
			LastTimestamp: metav1.NewTime(now.Add(-2 * time.Minute)),
		},
	)
	doc, err := (&Builder{Client: client}).Run(context.Background(), Request{
		Name: "ledger", Namespace: ns, Kind: "Pod", Prompt: "timeline ledger",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Timeline) < 2 {
		t.Fatalf("expected create + event, got %d", len(doc.Timeline))
	}
}

func hasCode(fs []incident.Finding, code string) bool {
	for _, f := range fs {
		if f.Code == code {
			return true
		}
	}
	return false
}
