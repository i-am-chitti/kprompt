package coordinator

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kprompt/kprompt/internal/agent/handoff"
)

func TestKubeProbeMergesIntoHandle(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "redis-0", Namespace: "platform"},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodReady, Status: corev1.ConditionFalse},
				},
				ContainerStatuses: []corev1.ContainerStatus{{RestartCount: 3}},
			},
		},
		&corev1.Event{
			ObjectMeta: metav1.ObjectMeta{Name: "ev1", Namespace: "platform"},
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "redis-0", Namespace: "platform"},
			Type:           corev1.EventTypeWarning,
			Reason:         "Unhealthy",
			Message:        "Readiness probe failed",
		},
	)
	svc := New()
	svc.Probe = &KubeProbe{Client: client}
	env := handoff.New("payments", "platform", "cross-ns dependency", sampleReport("payments", "timeout to redis"))
	reply, err := svc.Handle(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	if reply.MutateAttempted {
		t.Fatal("mutate must stay false")
	}
	foundProbe := false
	for _, r := range reply.Routing {
		if strings.Contains(r, "probed namespace platform") {
			foundProbe = true
		}
	}
	if !foundProbe {
		t.Fatalf("routing=%v", reply.Routing)
	}
	if len(reply.Merged.Evidence) == 0 {
		t.Fatal("expected merged evidence from probe")
	}
	if !strings.Contains(reply.Merged.Summary, "platform") {
		t.Fatalf("summary=%q", reply.Merged.Summary)
	}
}

func TestKubeProbeEmptyNS(t *testing.T) {
	p := &KubeProbe{Client: fake.NewSimpleClientset()}
	_, err := p.Probe(context.Background(), "", handoff.Envelope{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestKubeProbeHealthyEmitsEvidence(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "api-0", Namespace: "platform"},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodReady, Status: corev1.ConditionTrue},
				},
			},
		},
	)
	rep, err := (&KubeProbe{Client: client}).Probe(context.Background(), "platform", handoff.Envelope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Evidence) == 0 {
		t.Fatal("healthy probe must stamp at least one EvidenceRef (AG-068)")
	}
	if rep.Evidence[0].Source != "coordinator-kube-probe" {
		t.Fatalf("source=%q", rep.Evidence[0].Source)
	}
	origin := sampleReport("payments", "timeout")
	origin.Confidence = 0.9
	got := Merge(origin, rep, "platform")
	if got.Confidence > 0.45 {
		t.Fatalf("merged conf should be bounded by healthy probe, got %v", got.Confidence)
	}
	if strings.Contains(got.Reasoning, "unverified") {
		t.Fatalf("healthy probe evidence must verify, reasoning=%q", got.Reasoning)
	}
}
