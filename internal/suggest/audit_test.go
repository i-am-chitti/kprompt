package suggest

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kprompt/kprompt/internal/incident"
)

func auditFinding(code, title, kind, name, ns string) incident.Finding {
	return incident.Finding{
		Code:    code,
		Title:   title,
		Message: title,
		Evidence: []incident.EvidenceRef{{
			Type:     incident.EvidenceObject,
			Resource: &incident.ResourceRef{Kind: kind, Name: name, Namespace: ns},
		}},
	}
}

func TestFromAuditHardensPrivilegeGrants(t *testing.T) {
	priv, esc := true, true
	client := fake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "bad", Namespace: "payments"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name:  "app",
					Image: "nginx:1.27",
					SecurityContext: &corev1.SecurityContext{
						Privileged:               &priv,
						AllowPrivilegeEscalation: &esc,
					},
				}}},
			},
		},
	})
	inv := incident.Investigation{
		Namespace: "payments",
		Findings: []incident.Finding{
			auditFinding("Audit.Privileged", "Deployment/bad container app is privileged", "Deployment", "bad", "payments"),
			auditFinding("Audit.PrivilegeEscalation", "Deployment/bad container app allows privilege escalation", "Deployment", "bad", "payments"),
		},
	}
	suggestions, err := FromAudit(context.Background(), client, inv)
	if err != nil {
		t.Fatal(err)
	}
	actionable := ActionablePlans(suggestions)
	if len(actionable) != 1 {
		t.Fatalf("want 1 harden plan, got %d: %+v", len(actionable), suggestions)
	}
	plan := actionable[0].Plan
	if len(plan.Actions) != 1 {
		t.Fatalf("want 1 action, got %d", len(plan.Actions))
	}
	if !strings.Contains(plan.Actions[0].Diff, "privileged=false") ||
		!strings.Contains(plan.Actions[0].Diff, "allowPrivilegeEscalation=false") {
		t.Fatalf("diff=%q", plan.Actions[0].Diff)
	}
	if !strings.Contains(plan.Actions[0].Manifest, "privileged: false") {
		t.Fatalf("manifest missing privileged=false:\n%s", plan.Actions[0].Manifest)
	}
}

func TestFromAuditGuidanceOnlyForInventedValues(t *testing.T) {
	client := fake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "payments"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "web", Image: "nginx:latest"}}},
			},
		},
	})
	inv := incident.Investigation{
		Namespace: "payments",
		Findings: []incident.Finding{
			auditFinding("Audit.LatestTag", "Deployment/web container web uses a mutable image tag", "Deployment", "web", "payments"),
			auditFinding("Audit.MissingRequests", "Deployment/web container web is missing CPU/memory requests", "Deployment", "web", "payments"),
			auditFinding("Audit.RunAsRoot", "Deployment/web container web may run as root", "Deployment", "web", "payments"),
		},
	}
	suggestions, err := FromAudit(context.Background(), client, inv)
	if err != nil {
		t.Fatal(err)
	}
	if len(ActionablePlans(suggestions)) != 0 {
		t.Fatalf("invented-value findings must be guidance-only: %+v", suggestions)
	}
	if len(suggestions) != 3 {
		t.Fatalf("want 3 guidance suggestions, got %d: %+v", len(suggestions), suggestions)
	}
}

func TestFromAuditNonDeploymentIsGuidance(t *testing.T) {
	client := fake.NewSimpleClientset()
	inv := incident.Investigation{
		Namespace: "payments",
		Findings: []incident.Finding{
			auditFinding("Audit.Privileged", "DaemonSet/node-agent container agent is privileged", "DaemonSet", "node-agent", "payments"),
		},
	}
	suggestions, err := FromAudit(context.Background(), client, inv)
	if err != nil {
		t.Fatal(err)
	}
	if len(ActionablePlans(suggestions)) != 0 {
		t.Fatalf("DaemonSet harden must be guidance-only: %+v", suggestions)
	}
	if len(suggestions) != 1 {
		t.Fatalf("want 1 guidance, got %d", len(suggestions))
	}
}
