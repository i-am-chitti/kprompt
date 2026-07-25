package why

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kprompt/kprompt/internal/incident"
)

func TestWhyPendingPVCMissingStorageClass(t *testing.T) {
	ns := "payments"
	sc := "kprompt-nonexistent-storage"
	labels := map[string]string{"app": "ledger"}
	var replicas int32 = 1

	client := fake.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "ledger", Namespace: ns, UID: "d1"},
			Spec: appsv1.DeploymentSpec{
				Replicas: &replicas,
				Selector: &metav1.LabelSelector{MatchLabels: labels},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: labels},
					Spec: corev1.PodSpec{
						Volumes: []corev1.Volume{{
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: "ledger-data",
								},
							},
						}},
						Containers: []corev1.Container{{Name: "ledger", Image: "busybox"}},
					},
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "ledger-1", Namespace: ns, Labels: labels},
			Spec: corev1.PodSpec{
				Volumes: []corev1.Volume{{
					Name: "data",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "ledger-data",
						},
					},
				}},
				Containers: []corev1.Container{{Name: "ledger", Image: "busybox"}},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
				Conditions: []corev1.PodCondition{{
					Type:   corev1.PodScheduled,
					Status: corev1.ConditionFalse,
					Reason: "Unschedulable",
					Message: "0/1 nodes are available: pod has unbound immediate PersistentVolumeClaims.",
				}},
			},
		},
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "ledger-data", Namespace: ns},
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: &sc,
				AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			},
			Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending},
		},
	)

	doc, err := (&Analyzer{Client: client}).Run(context.Background(), Request{
		Name: "ledger", Namespace: ns, Kind: "Deployment",
		Prompt: "why is ledger Pending",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := incident.ValidateInvestigation(doc); err != nil {
		t.Fatal(err)
	}
	if !hasCode(doc, "Symptom.Pending") || !hasCode(doc, "Cause.MissingStorageClass") {
		t.Fatalf("findings: %+v", doc.Findings)
	}
	if doc.Confidence < 0.85 {
		t.Fatalf("confidence: %v", doc.Confidence)
	}
	if !stringsContains(doc.Summary, "→") {
		t.Fatalf("summary should be a cause chain: %s", doc.Summary)
	}
}

func TestWhyCrashLoop(t *testing.T) {
	ns := "payments"
	labels := map[string]string{"app": "api"}
	var replicas int32 = 1
	client := fake.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: ns, UID: "d1"},
			Spec: appsv1.DeploymentSpec{
				Replicas: &replicas,
				Selector: &metav1.LabelSelector{MatchLabels: labels},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: labels},
					Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "api", Image: "busybox"}}},
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "api-1", Namespace: ns, Labels: labels},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{
					Name: "api", Ready: false, RestartCount: 4,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
					},
					LastTerminationState: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{Reason: "Error", ExitCode: 1},
					},
				}},
			},
		},
	)
	doc, err := (&Analyzer{Client: client}).Run(context.Background(), Request{
		Name: "api", Namespace: ns, Kind: "Deployment",
		Prompt: "why is api crashlooping",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasCode(doc, "Symptom.CrashLoop") || !hasCode(doc, "Cause.ExitNonZero") {
		t.Fatalf("findings: %+v", doc.Findings)
	}
}

func TestWhyImagePull(t *testing.T) {
	ns := "payments"
	client := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-1", Namespace: ns},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			ContainerStatuses: []corev1.ContainerStatus{{
				Name: "worker",
				State: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{
						Reason:  "ImagePullBackOff",
						Message: "Back-off pulling image",
					},
				},
			}},
		},
	})
	doc, err := (&Analyzer{Client: client}).Run(context.Background(), Request{
		Name: "worker-1", Namespace: ns, Kind: "Pod",
		Prompt: "why is worker-1 pending",
	})
	if err != nil {
		t.Fatal(err)
	}
	// ImagePull waiting wins over Pending phase path.
	if !hasCode(doc, "Symptom.ImagePull") || !hasCode(doc, "Cause.BadImageRef") {
		t.Fatalf("findings: %+v", doc.Findings)
	}
}

func hasCode(doc incident.Investigation, code string) bool {
	for _, f := range doc.Findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

func stringsContains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		})())
}
