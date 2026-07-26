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

func TestSuggestProbeRelaxesTiming(t *testing.T) {
	client := fake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "demo"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name:  "web",
					Image: "nginx",
					ReadinessProbe: &corev1.Probe{
						ProbeHandler:        corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/ready"}},
						InitialDelaySeconds: 5,
						FailureThreshold:    3,
					},
				}}},
			},
		},
	})
	rep := cluster.ExplainReport{
		Kind: "Deployment", Target: "web", Namespace: "demo",
		Findings: []cluster.Finding{{Code: "ProbeFailure", Container: "web", Message: "Readiness probe failed"}},
	}
	suggestions, err := FromExplain(context.Background(), client, rep, "")
	if err != nil {
		t.Fatal(err)
	}
	actionable := ActionablePlans(suggestions)
	if len(actionable) != 1 {
		t.Fatalf("suggestions=%+v", suggestions)
	}
	if !strings.Contains(actionable[0].Summary, "initialDelay 5→10") {
		t.Fatalf("summary=%q", actionable[0].Summary)
	}
	if !strings.Contains(actionable[0].Summary, "failureThreshold 3→5") {
		t.Fatalf("summary=%q", actionable[0].Summary)
	}
}

func TestSuggestGuidanceForMissingStorageClass(t *testing.T) {
	inv := incident.Investigation{
		Namespace: "payments",
		Target:    &incident.ResourceRef{Kind: "Deployment", Name: "ledger", Namespace: "payments"},
		Findings: []incident.Finding{
			{Code: "Symptom.Pending", Message: "Pod/ledger-1 phase=Pending"},
			{Code: "Cause.MissingStorageClass", Message: `StorageClass "missing" is missing`},
		},
	}
	suggestions, err := FromInvestigation(context.Background(), fake.NewSimpleClientset(), inv, "why is ledger Pending")
	if err != nil {
		t.Fatal(err)
	}
	if len(ActionablePlans(suggestions)) != 0 {
		t.Fatalf("storage guidance must not invent a mutating plan: %+v", suggestions)
	}
	if len(suggestions) == 0 {
		t.Fatal("expected guidance suggestions")
	}
	found := false
	for _, s := range suggestions {
		if s.Code == "MissingStorageClass" {
			found = true
			if s.Plan != nil {
				t.Fatal("MissingStorageClass must be guidance-only")
			}
		}
	}
	if !found {
		t.Fatalf("suggestions=%+v", suggestions)
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

func TestSuggestOOMOnStatefulSet(t *testing.T) {
	limit := resource.MustParse("256Mi")
	client := fake.NewSimpleClientset(&appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "demo"},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "db"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "db"}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "postgres",
						Image: "postgres:16",
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{corev1.ResourceMemory: limit},
						},
					}},
				},
			},
		},
	})
	suggestions, err := FromExplain(context.Background(), client, cluster.ExplainReport{
		Target: "db", Namespace: "demo", Kind: "StatefulSet",
		Findings: []cluster.Finding{{Code: "OOMKilled", Container: "postgres", Severity: "error"}},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(suggestions) != 1 || suggestions[0].Plan == nil {
		t.Fatalf("%+v", suggestions)
	}
	if suggestions[0].Plan.Actions[0].Object.Kind != "StatefulSet" {
		t.Fatalf("kind=%s", suggestions[0].Plan.Actions[0].Object.Kind)
	}
	if !strings.Contains(suggestions[0].Plan.Summary, "StatefulSet/db") {
		t.Fatalf("summary=%s", suggestions[0].Plan.Summary)
	}
}

func TestSuggestImagePullOnDaemonSet(t *testing.T) {
	client := fake.NewSimpleClientset(&appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "demo"},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "agent"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "agent"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "agent", Image: "agent:bad"}}},
			},
		},
	})
	suggestions, err := FromExplain(context.Background(), client, cluster.ExplainReport{
		Target: "agent", Namespace: "demo", Kind: "DaemonSet",
		Findings: []cluster.Finding{{Code: "ImagePullBackOff", Container: "agent"}},
	}, `set agent image to ghcr.io/example/agent:1.0.0`)
	if err != nil {
		t.Fatal(err)
	}
	if len(suggestions) != 1 || suggestions[0].Plan == nil {
		t.Fatalf("%+v", suggestions)
	}
	if suggestions[0].Plan.Actions[0].Object.Kind != "DaemonSet" {
		t.Fatalf("kind=%s", suggestions[0].Plan.Actions[0].Object.Kind)
	}
}

func TestSuggestImagePullAuthIsGuidance(t *testing.T) {
	client := fake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "demo"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "api"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "private/app:1"}}},
			},
		},
	})
	suggestions, err := FromExplain(context.Background(), client, cluster.ExplainReport{
		Target: "api", Namespace: "demo", Kind: "Deployment",
		Findings: []cluster.Finding{{
			Code: "ImagePullBackOff", Container: "app",
			Message: `Failed to pull image "private/app:1": unauthorized: authentication required`,
		}},
	}, `set api image to private/app:2`)
	if err != nil {
		t.Fatal(err)
	}
	if len(suggestions) != 1 || suggestions[0].Plan != nil {
		t.Fatalf("auth ImagePull must stay guidance: %+v", suggestions)
	}
	if !strings.Contains(strings.ToLower(suggestions[0].Title), "pullsecret") &&
		!strings.Contains(strings.ToLower(suggestions[0].Summary), "auth") {
		t.Fatalf("expected auth guidance, got %+v", suggestions[0])
	}
}

func TestSuggestCrashLoopSkippedWhenOOMPresent(t *testing.T) {
	ctrl := true
	depUID := types.UID("dep-oom")
	client := fake.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "demo", UID: depUID},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "api"}},
					Spec: corev1.PodSpec{Containers: []corev1.Container{{
						Name:  "app",
						Image: "app:2",
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("64Mi")},
						},
					}}},
				},
			},
		},
		&appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "api-1", Namespace: "demo",
				Annotations:     map[string]string{"deployment.kubernetes.io/revision": "1"},
				OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: "api", UID: depUID, Controller: &ctrl}},
			},
		},
		&appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "api-2", Namespace: "demo",
				Annotations:     map[string]string{"deployment.kubernetes.io/revision": "2"},
				OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: "api", UID: depUID, Controller: &ctrl}},
			},
		},
	)
	suggestions, err := FromExplain(context.Background(), client, cluster.ExplainReport{
		Target: "api", Namespace: "demo", Kind: "Deployment",
		Findings: []cluster.Finding{
			{Code: "CrashLoopBackOff", Container: "app"},
			{Code: "OOMKilled", Container: "app"},
		},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	actionable := ActionablePlans(suggestions)
	if len(actionable) != 1 || actionable[0].Code != "OOMKilled" {
		t.Fatalf("want only OOM plan, got %+v", suggestions)
	}
	for _, s := range suggestions {
		if s.Code == "CrashLoopBackOff" {
			t.Fatalf("CrashLoop must be skipped when OOM present: %+v", suggestions)
		}
	}
}
