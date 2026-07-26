package suggest

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kprompt/kprompt/internal/cluster"
	"github.com/kprompt/kprompt/internal/incident"
	"github.com/kprompt/kprompt/internal/planner"
)

func TestSuggestOOMRaisesMemory(t *testing.T) {
	limit := resource.MustParse("128Mi")
	client := fake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "demo"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "api"}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "app",
						Image: "app:1",
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{corev1.ResourceMemory: limit},
						},
					}},
				},
			},
		},
	})
	suggestions, err := FromExplain(context.Background(), client, cluster.ExplainReport{
		Target: "api", Namespace: "demo", Kind: "Deployment",
		Findings: []cluster.Finding{{Code: "OOMKilled", Container: "app", Severity: "error"}},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(suggestions) != 1 || suggestions[0].Plan == nil {
		t.Fatalf("%+v", suggestions)
	}
	if !strings.Contains(suggestions[0].Plan.Summary, "128Mi") {
		t.Fatalf("summary=%s", suggestions[0].Plan.Summary)
	}
}

func TestSuggestCrashLoopPromptOnlyWithoutHistory(t *testing.T) {
	client := fake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "crashy", Namespace: "demo", UID: "dep1"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "crashy"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "crashy"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "crash", Image: "app:bad"}}},
			},
		},
	})
	suggestions, err := FromExplain(context.Background(), client, cluster.ExplainReport{
		Target: "crashy", Namespace: "demo", Kind: "Deployment",
		Findings: []cluster.Finding{{Code: "CrashLoopBackOff", Container: "crash"}},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(suggestions) != 1 || suggestions[0].Plan != nil {
		t.Fatalf("%+v", suggestions)
	}
	if suggestions[0].Prompt != "logs crashy" {
		t.Fatalf("prompt=%q", suggestions[0].Prompt)
	}
}

func TestSuggestCrashLoopRollbackWithHistory(t *testing.T) {
	ctrl := true
	depUID := types.UID("dep1")
	client := fake.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "demo", UID: depUID},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "api"}},
					Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "app:2"}}},
				},
			},
		},
		&appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "api-v1", Namespace: "demo",
				Annotations:     map[string]string{"deployment.kubernetes.io/revision": "1"},
				OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: "api", UID: depUID, Controller: &ctrl}},
			},
		},
		&appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "api-v2", Namespace: "demo",
				Annotations:     map[string]string{"deployment.kubernetes.io/revision": "2"},
				OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: "api", UID: depUID, Controller: &ctrl}},
			},
		},
	)
	suggestions, err := FromExplain(context.Background(), client, cluster.ExplainReport{
		Target: "api", Namespace: "demo", Kind: "Deployment",
		Findings: []cluster.Finding{{Code: "CrashLoopBackOff", Container: "app"}},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(suggestions) != 1 || suggestions[0].Plan == nil {
		t.Fatalf("%+v", suggestions)
	}
	if suggestions[0].Plan.Actions[0].Op != planner.OpRollback {
		t.Fatalf("op=%s", suggestions[0].Plan.Actions[0].Op)
	}
}

func TestSuggestImagePullNeedsNamedReplacement(t *testing.T) {
	client := fake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "demo"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "worker"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "worker"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "worker", Image: "ghcr.io/bad/missing:9.9.9"}}},
			},
		},
	})
	noPlan, err := FromExplain(context.Background(), client, cluster.ExplainReport{
		Target: "worker", Namespace: "demo", Kind: "Deployment",
		Findings: []cluster.Finding{{Code: "ImagePullBackOff", Container: "worker"}},
	}, `explain why worker is ImagePullBackOff`)
	if err != nil {
		t.Fatal(err)
	}
	if len(noPlan) != 1 || noPlan[0].Plan != nil {
		t.Fatalf("expected guidance only: %+v", noPlan)
	}

	withPlan, err := FromExplain(context.Background(), client, cluster.ExplainReport{
		Target: "worker", Namespace: "demo", Kind: "Deployment",
		Findings: []cluster.Finding{{Code: "ImagePullBackOff", Container: "worker"}},
	}, `set worker image to ghcr.io/example/worker:1.2.3`)
	if err != nil {
		t.Fatal(err)
	}
	if len(withPlan) != 1 || withPlan[0].Plan == nil {
		t.Fatalf("%+v", withPlan)
	}
	if !strings.Contains(withPlan[0].Plan.Summary, "ghcr.io/example/worker:1.2.3") {
		t.Fatalf("summary=%s", withPlan[0].Plan.Summary)
	}
}

func TestFromInvestigationMapsWhyFindings(t *testing.T) {
	ctrl := true
	depUID := types.UID("dep1")
	client := fake.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "payments", UID: depUID},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "api"}},
					Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "api", Image: "busybox:bad"}}},
				},
			},
		},
		&appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "api-1", Namespace: "payments",
				Annotations:     map[string]string{"deployment.kubernetes.io/revision": "1"},
				OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "api", UID: depUID, Controller: &ctrl}},
			},
		},
		&appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "api-2", Namespace: "payments",
				Annotations:     map[string]string{"deployment.kubernetes.io/revision": "2"},
				OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "api", UID: depUID, Controller: &ctrl}},
			},
		},
	)
	inv := incident.Investigation{
		Namespace: "payments",
		Target:    &incident.ResourceRef{Kind: "Deployment", Name: "api", Namespace: "payments"},
		Findings: []incident.Finding{
			{Code: "Symptom.CrashLoop", Message: "container api is in CrashLoopBackOff"},
			{Code: "Cause.ExitNonZero", Message: "container api last exit code 1"},
		},
	}
	suggestions, err := FromInvestigation(context.Background(), client, inv, "why is api crashing")
	if err != nil {
		t.Fatal(err)
	}
	actionable := ActionablePlans(suggestions)
	if len(actionable) != 1 {
		t.Fatalf("suggestions=%+v", suggestions)
	}
}

func TestFromInvestigationImagePullDedupesSymptomAndCause(t *testing.T) {
	client := fake.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "payments"},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "worker", Image: "bad:9.9.9"}}},
				},
			},
		},
	)
	inv := incident.Investigation{
		Namespace: "payments",
		Target:    &incident.ResourceRef{Kind: "Deployment", Name: "worker", Namespace: "payments"},
		Findings: []incident.Finding{
			{Code: "Symptom.ImagePull", Message: `container worker: Back-off pulling image "bad:9.9.9"`},
			{Code: "Cause.BadImageRef", Message: "Verify image name/tag and imagePullSecrets"},
		},
	}
	suggestions, err := FromInvestigation(context.Background(), client, inv, "set worker image to busybox:1.36")
	if err != nil {
		t.Fatal(err)
	}
	actionable := ActionablePlans(suggestions)
	if len(actionable) != 1 {
		t.Fatalf("want 1 plan, got %d suggestions=%+v", len(actionable), suggestions)
	}
	if !strings.Contains(actionable[0].Summary, "container worker") {
		t.Fatalf("summary=%q", actionable[0].Summary)
	}
}

func TestExtractReplacementImage(t *testing.T) {
	got := extractReplacementImage(`set worker image to ghcr.io/example/worker:1.2.3`)
	if got != "ghcr.io/example/worker:1.2.3" {
		t.Fatalf("got %q", got)
	}
	if extractReplacementImage("why is worker ImagePullBackOff") != "" {
		t.Fatal("expected empty")
	}
}

func TestBumpMemoryDefault(t *testing.T) {
	old, neu := bumpMemory(nil)
	if !old.IsZero() || neu.String() != "256Mi" {
		t.Fatalf("old=%s new=%s", old.String(), neu.String())
	}
}
