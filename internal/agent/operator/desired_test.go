package operator

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentv1 "github.com/kprompt/kprompt/api/v1"
)

func TestBuildDesiredObserve(t *testing.T) {
	cr := &agentv1.KpromptAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "payments", UID: "uid-1"},
		Spec: agentv1.KpromptAgentSpec{
			Mode:      agentv1.ModeObserve,
			LLM:       agentv1.LLMSpec{Provider: "openai", Heuristic: true},
			Notify:    agentv1.NotifySpec{Slack: true, Discord: true},
			SecretRef: &agentv1.SecretRef{Name: "kprompt-agent"},
			Watches:   []string{"pods", "events", "deployments"},
		},
	}
	d, err := BuildDesired(cr, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if d.Deployment.Namespace != "payments" || d.SA.Name != "kprompt-agent-demo" {
		t.Fatalf("ns/name: %+v %+v", d.Deployment.Namespace, d.SA.Name)
	}
	args := d.Deployment.Spec.Template.Spec.Containers[0].Args
	joined := ""
	for _, a := range args {
		joined += a + " "
	}
	for _, want := range []string{"--in-cluster", "--heuristic", "--slack", "--discord", "--watch", "--agent-cr"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args missing %s: %v", want, args)
		}
	}
	if len(d.Role.Rules) < 4 {
		t.Fatalf("expected observe rules, got %d", len(d.Role.Rules))
	}
}

func TestBuildDesiredRoleNotClusterRole(t *testing.T) {
	cr := &agentv1.KpromptAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "payments", UID: "uid-2"},
		Spec: agentv1.KpromptAgentSpec{
			Mode:    agentv1.ModeObserve,
			LLM:     agentv1.LLMSpec{Heuristic: true},
			Watches: []string{"pods", "events", "deployments"},
		},
	}
	d, err := BuildDesired(cr, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if d.Role == nil {
		t.Fatal("expected Role")
	}
	if d.RoleBinding == nil || d.RoleBinding.RoleRef.Kind != "Role" {
		t.Fatalf("expected RoleBinding→Role, got %+v", d.RoleBinding)
	}
	if d.Role.Namespace != "payments" || d.RoleBinding.Namespace != "payments" {
		t.Fatalf("Role must stay in watch ns: role=%q rb=%q", d.Role.Namespace, d.RoleBinding.Namespace)
	}
	mutate := map[string]bool{"create": true, "update": true, "patch": true, "delete": true}
	for _, rule := range d.Role.Rules {
		for _, res := range rule.Resources {
			r := strings.ToLower(res)
			if r == "pods" || r == "deployments" || r == "services" || r == "secrets" {
				for _, v := range rule.Verbs {
					if mutate[strings.ToLower(v)] {
						t.Fatalf("observe Role must not mutate %s via %s: %+v", res, v, rule)
					}
				}
			}
		}
	}
}

func TestRejectAutopilot(t *testing.T) {
	cr := &agentv1.KpromptAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "ns"},
		Spec:       agentv1.KpromptAgentSpec{Mode: "Autopilot"},
	}
	if _, err := BuildDesired(cr, Options{}); err == nil {
		t.Fatal("expected Autopilot reject")
	}
}

func TestRejectCrossNamespace(t *testing.T) {
	cr := &agentv1.KpromptAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "ops"},
		Spec:       agentv1.KpromptAgentSpec{Mode: agentv1.ModeObserve, Namespace: "payments"},
	}
	if _, err := BuildDesired(cr, Options{}); err == nil {
		t.Fatal("expected cross-ns reject")
	}
}

func TestDefaultModeEmpty(t *testing.T) {
	cr := &agentv1.KpromptAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "ns"},
		Spec:       agentv1.KpromptAgentSpec{},
	}
	if err := ValidateMode(cr); err != nil {
		t.Fatal(err)
	}
	d, err := BuildDesired(cr, Options{DefaultImage: "example.com/kprompt:dev"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Deployment.Spec.Template.Spec.Containers[0].Image != "example.com/kprompt:dev" {
		t.Fatalf("image=%s", d.Deployment.Spec.Template.Spec.Containers[0].Image)
	}
}
