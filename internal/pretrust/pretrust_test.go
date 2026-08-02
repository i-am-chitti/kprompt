package pretrust

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kprompt/kprompt/internal/incident"
	"github.com/kprompt/kprompt/internal/intent"
	"github.com/kprompt/kprompt/internal/planner"
)

func sampleInv(conf float64) incident.Investigation {
	inv := incident.NewInvestigation("investigate api", "payments")
	inv.Summary = "CrashLoop on api"
	inv.RootCause = "CrashLoopBackOff"
	inv.Confidence = conf
	inv.Target = &incident.ResourceRef{Kind: "Deployment", Name: "api", Namespace: "payments"}
	inv.Findings = []incident.Finding{{
		Code: "CrashLoopBackOff", Severity: incident.SeverityHigh,
		Title: "Crash looping", Message: "container in CrashLoopBackOff",
	}}
	return inv
}

func TestHighConfidenceWithoutEvidenceCaps(t *testing.T) {
	inv := sampleInv(0.85)
	rep := Investigation(context.Background(), nil, inv)
	if rep.OK {
		t.Fatal("expected fail without evidence")
	}
	Apply(&inv, rep)
	if inv.Confidence > SoftAgreeConfidenceCap {
		t.Fatalf("confidence=%v", inv.Confidence)
	}
	if len(inv.Degraded) == 0 || !strings.Contains(strings.Join(inv.Degraded, " "), "EvidenceRef") {
		t.Fatalf("degraded=%v", inv.Degraded)
	}
}

func TestEvidencePresentKeepsConfidence(t *testing.T) {
	inv := sampleInv(0.85)
	inv.Evidence = []incident.EvidenceRef{{
		Type: incident.EvidenceEvent, Reason: "BackOff", Message: "CrashLoopBackOff",
		Resource: &incident.ResourceRef{Kind: "Pod", Name: "api-0", Namespace: "payments"},
		Source:   "kubectl",
	}}
	rep := Investigation(context.Background(), nil, inv)
	if !rep.OK {
		t.Fatalf("%+v", rep)
	}
	Apply(&inv, rep)
	if inv.Confidence != 0.85 {
		t.Fatalf("confidence=%v", inv.Confidence)
	}
}

func TestRereadContradictsCrashLoop(t *testing.T) {
	inv := sampleInv(0.85)
	inv.Evidence = []incident.EvidenceRef{{
		Type: incident.EvidenceEvent, Reason: "BackOff", Message: "CrashLoopBackOff",
		Resource: &incident.ResourceRef{Kind: "Pod", Name: "api-0"},
	}}
	client := fake.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "payments"},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "api-0", Namespace: "payments", Labels: map[string]string{"app": "api"}},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodReady, Status: corev1.ConditionTrue},
				},
				ContainerStatuses: []corev1.ContainerStatus{{
					Name: "api", Ready: true,
					State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
				}},
			},
		},
	)
	rep := Investigation(context.Background(), client, inv)
	if rep.OK {
		t.Fatalf("expected re-read fail: %+v", rep)
	}
	Apply(&inv, rep)
	if inv.Confidence > SoftAgreeConfidenceCap {
		t.Fatalf("confidence=%v", inv.Confidence)
	}
}

func TestRereadConfirmsImagePull(t *testing.T) {
	inv := sampleInv(0.85)
	inv.RootCause = "ImagePullBackOff"
	inv.Findings[0].Code = "ImagePullBackOff"
	inv.Findings[0].Message = "ImagePullBackOff"
	inv.Evidence = []incident.EvidenceRef{{
		Type: incident.EvidenceEvent, Reason: "Failed", Message: "ImagePullBackOff",
		Resource: &incident.ResourceRef{Kind: "Pod", Name: "api-0"},
	}}
	client := fake.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "payments"},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "api-0", Namespace: "payments", Labels: map[string]string{"app": "api"}},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
				ContainerStatuses: []corev1.ContainerStatus{{
					Name: "api", Ready: false,
					State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}},
				}},
			},
		},
	)
	rep := Investigation(context.Background(), client, inv)
	if !rep.OK {
		t.Fatalf("%+v", rep)
	}
}

func TestSuggestedPlanHardDeny(t *testing.T) {
	inv := sampleInv(0.5)
	inv.Evidence = []incident.EvidenceRef{{Type: incident.EvidenceObject, Message: "ok", Source: "t"}}
	plan := planner.ExecutionPlan{
		Summary: "delete namespace payments",
		Intent: intent.Intent{
			Kind: intent.KindDelete,
			Target: intent.Target{Kind: "Namespace", Name: "payments"},
		},
		RequiresApproval: true,
		Actions: []planner.Action{{
			Op:     planner.OpDelete,
			Object: planner.ObjectRef{Kind: "Namespace", Name: "payments"},
		}},
	}
	rep := SuggestedPlan(context.Background(), nil, inv, plan)
	if !rep.Denied {
		t.Fatalf("expected hard deny, got %+v", rep)
	}
}

func TestSuggestedPlanEvidenceGate(t *testing.T) {
	inv := sampleInv(0.9) // no evidence
	plan := planner.ExecutionPlan{
		Summary:          "patch memory",
		RequiresApproval: true,
		Intent:           intent.Intent{Kind: intent.KindPatch},
		Actions: []planner.Action{{
			Op:     planner.OpUpdate,
			Object: planner.ObjectRef{Kind: "Deployment", Name: "api", Namespace: "payments"},
		}},
	}
	rep := SuggestedPlan(context.Background(), nil, inv, plan)
	if rep.OK {
		t.Fatal("expected evidence-gate fail")
	}
}

func TestApplyNeverRaises(t *testing.T) {
	inv := sampleInv(0.3)
	Apply(&inv, Report{OK: true, ConfidenceCap: 0.9})
	if inv.Confidence != 0.3 {
		t.Fatalf("must not raise confidence, got %v", inv.Confidence)
	}
}
