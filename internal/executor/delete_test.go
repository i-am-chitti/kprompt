package executor

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kprompt/kprompt/internal/planner"
)

func TestDeleteDeployment(t *testing.T) {
	client := fake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "redis", Namespace: "demo"},
	})
	err := (&Runner{Client: client}).Apply(context.Background(), planner.ExecutionPlan{
		Actions: []planner.Action{{
			Op: planner.OpDelete,
			Object: planner.ObjectRef{
				Kind: "Deployment", Name: "redis", Namespace: "demo",
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.AppsV1().Deployments("demo").Get(context.Background(), "redis", metav1.GetOptions{})
	if err == nil {
		t.Fatal("expected deployment deleted")
	}
}

func TestDeleteJobAndReplicaSet(t *testing.T) {
	client := fake.NewSimpleClientset(
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "old-migrate", Namespace: "payments"}},
		&appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "api-old", Namespace: "payments"}},
	)
	err := (&Runner{Client: client}).Apply(context.Background(), planner.ExecutionPlan{
		Actions: []planner.Action{
			{Op: planner.OpDelete, Object: planner.ObjectRef{Kind: "Job", Name: "old-migrate", Namespace: "payments"}},
			{Op: planner.OpDelete, Object: planner.ObjectRef{Kind: "ReplicaSet", Name: "api-old", Namespace: "payments"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.BatchV1().Jobs("payments").Get(context.Background(), "old-migrate", metav1.GetOptions{}); err == nil {
		t.Fatal("expected Job deleted")
	}
	if _, err := client.AppsV1().ReplicaSets("payments").Get(context.Background(), "api-old", metav1.GetOptions{}); err == nil {
		t.Fatal("expected ReplicaSet deleted")
	}
}
