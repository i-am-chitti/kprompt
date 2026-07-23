package verify

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kprompt/kprompt/internal/planner"
)

func TestPlanScaleReady(t *testing.T) {
	rep := int32(3)
	client := fake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "demo", Generation: 2},
		Spec:       appsv1.DeploymentSpec{Replicas: &rep},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 2,
			Replicas:           3,
			UpdatedReplicas:    3,
			AvailableReplicas:  3,
		},
	})
	want := int32(3)
	report := Plan(context.Background(), client, planner.ExecutionPlan{
		RequiresApproval: true,
		Actions: []planner.Action{{
			Op:       planner.OpScale,
			Object:   planner.ObjectRef{Kind: "Deployment", Name: "api", Namespace: "demo"},
			Replicas: &want,
		}},
	})
	if report.Status != OK {
		t.Fatalf("%+v", report)
	}
}

func TestPlanScalePending(t *testing.T) {
	rep := int32(3)
	client := fake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "demo", Generation: 2},
		Spec:       appsv1.DeploymentSpec{Replicas: &rep},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 2,
			Replicas:           1,
			UpdatedReplicas:    1,
			AvailableReplicas:  1,
		},
	})
	want := int32(3)
	report := Plan(context.Background(), client, planner.ExecutionPlan{
		RequiresApproval: true,
		Actions: []planner.Action{{
			Op:       planner.OpScale,
			Object:   planner.ObjectRef{Kind: "Deployment", Name: "api", Namespace: "demo"},
			Replicas: &want,
		}},
	})
	if report.Status != Pending {
		t.Fatalf("%+v", report)
	}
}

func TestPlanDeleteGone(t *testing.T) {
	client := fake.NewSimpleClientset()
	report := Plan(context.Background(), client, planner.ExecutionPlan{
		RequiresApproval: true,
		Actions: []planner.Action{{
			Op:     planner.OpDelete,
			Object: planner.ObjectRef{Kind: "Pod", Name: "x", Namespace: "demo"},
		}},
	})
	if report.Status != OK {
		t.Fatalf("%+v", report)
	}
}

func TestPlanDeleteStillPresent(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "demo"},
	})
	report := Plan(context.Background(), client, planner.ExecutionPlan{
		RequiresApproval: true,
		Actions: []planner.Action{{
			Op:     planner.OpDelete,
			Object: planner.ObjectRef{Kind: "Pod", Name: "x", Namespace: "demo"},
		}},
	})
	if report.Status != Failed {
		t.Fatalf("%+v", report)
	}
}
