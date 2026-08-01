package fleet

import (
	"context"
	"strings"
	"testing"

	agentv1 "github.com/kprompt/kprompt/api/v1"
	"github.com/kprompt/kprompt/internal/agent/operator"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestListCRAndDeployment(t *testing.T) {
	score := 72
	cr := &agentv1.KpromptAgent{
		TypeMeta: metav1.TypeMeta{APIVersion: agentv1.Group + "/" + agentv1.Version, Kind: agentv1.Kind},
		ObjectMeta: metav1.ObjectMeta{Name: "payments-agent", Namespace: "payments"},
		Spec:       agentv1.KpromptAgentSpec{Mode: agentv1.ModeObserve},
		Status: agentv1.KpromptAgentStatus{
			HealthScore: &score,
			HealthTrend: "stable",
			Conditions:  []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}},
		},
	}
	u, err := toUnstructured(cr)
	if err != nil {
		t.Fatal(err)
	}

	scheme := runtime.NewScheme()
	_ = appsv1.AddToScheme(scheme)
	listKinds := map[schema.GroupVersionResource]string{
		gvr: agentv1.Kind + "List",
	}
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(scheme, listKinds, u)

	replicas := int32(1)
	cs := k8sfake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kprompt-agent-payments-agent",
			Namespace: "payments",
			Labels:    map[string]string{operator.LabelName: operator.AppName},
		},
		Spec: appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{ReadyReplicas: 1},
	}, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kprompt-agent-orphan",
			Namespace: "platform",
			Labels:    map[string]string{operator.LabelName: operator.AppName},
		},
		Spec: appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{ReadyReplicas: 0},
	})

	inv, err := List(context.Background(), dyn, cs, ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Kind != kindInventory {
		t.Fatalf("%+v", inv)
	}
	// CR + orphan deployment (owned deploy deduped)
	if len(inv.Agents) != 2 {
		t.Fatalf("agents=%+v", inv.Agents)
	}
	text := Format(inv)
	if !strings.Contains(text, "payments/payments-agent") || !strings.Contains(text, "platform/kprompt-agent-orphan") {
		t.Fatalf("format=%q", text)
	}
	if strings.Contains(text, "kprompt-agent-payments-agent") {
		t.Fatalf("expected owned deployment deduped: %q", text)
	}
}

func toUnstructured(cr *agentv1.KpromptAgent) (*unstructured.Unstructured, error) {
	b, err := runtime.DefaultUnstructuredConverter.ToUnstructured(cr)
	if err != nil {
		return nil, err
	}
	u := &unstructured.Unstructured{Object: b}
	u.SetAPIVersion(agentv1.Group + "/" + agentv1.Version)
	u.SetKind(agentv1.Kind)
	u.SetName(cr.Name)
	u.SetNamespace(cr.Namespace)
	return u, nil
}
