package architecture

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kprompt/kprompt/internal/agent/memory"
	"github.com/kprompt/kprompt/internal/graph"
	"github.com/kprompt/kprompt/internal/learn"
)

func TestFromSignalsPaymentishShape(t *testing.T) {
	prof := learn.Profile{
		Tools: []learn.ToolEntry{
			{ID: "gateway-api", Name: "Gateway API", Available: true, Detail: "CRD present"},
			{ID: "gitops", Name: "GitOps", Available: true, Detail: "Argo CD Application CRD"},
			{ID: "prometheus", Name: "Prometheus", Available: true},
		},
	}
	gRep := graph.Report{
		Type: "service-graph",
		Nodes: []graph.Node{
			{Kind: graph.NodeService, Name: "api", Namespace: "payments"},
			{Kind: graph.NodeService, Name: "redis", Namespace: "payments"},
			{Kind: graph.NodeIngress, Name: "api-ing", Namespace: "payments"},
		},
		Edges: []graph.Edge{
			{Type: graph.EdgeExposes},
			{Type: graph.EdgeSelects},
		},
	}
	deps := []memory.Fact{{
		Kind: memory.KindDependency, Key: "redis", Value: "service/redis",
		Evidence: "Service name hints at redis",
	}, {
		Kind: memory.KindDependency, Key: "kafka", Value: "service/kafka",
		Evidence: "Service name hints at kafka",
	}}

	got := FromSignals(prof, gRep, deps, "payments")
	if got.Type != TypeArchitecture {
		t.Fatalf("type=%q", got.Type)
	}
	if got.Confidence != ConfidenceHigh {
		t.Fatalf("confidence=%q", got.Confidence)
	}
	if !strings.Contains(got.Narrative, "Gateway API") || !strings.Contains(got.Narrative, "GitOps") {
		t.Fatalf("narrative=%q", got.Narrative)
	}
	if !strings.Contains(got.Narrative, "Redis") || !strings.Contains(got.Narrative, "Kafka") {
		t.Fatalf("expected data deps in narrative: %q", got.Narrative)
	}
	if len(got.Components) < 4 {
		t.Fatalf("components=%+v", got.Components)
	}
}

func TestFromSignalsThinProfile(t *testing.T) {
	got := FromSignals(learn.Profile{}, graph.Report{
		Nodes: []graph.Node{{Kind: graph.NodeService, Name: "web", Namespace: "default"}},
	}, nil, "default")
	if got.Confidence != ConfidenceLow {
		t.Fatalf("confidence=%q", got.Confidence)
	}
	if !strings.Contains(strings.ToLower(got.Narrative), "thin") &&
		!strings.Contains(got.Narrative, "basic Kubernetes") {
		t.Fatalf("narrative=%q", got.Narrative)
	}
	if len(got.Hints) == 0 {
		t.Fatal("expected hints for thin profile")
	}
}

func TestAnalyzerRun(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "redis-master", Namespace: "shop"}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "shop"}},
	)
	prof := learn.Profile{
		Tools: []learn.ToolEntry{
			{ID: "helm", Name: "Helm", Available: true},
		},
	}
	got, err := (&Analyzer{Client: client}).Run(context.Background(), Request{
		Namespace: "shop",
		Prompt:    "explain architecture",
		Profile:   &prof,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Narrative == "" {
		t.Fatal("empty narrative")
	}
	foundRedis := false
	for _, c := range got.Components {
		if c.Name == "Redis" {
			foundRedis = true
		}
	}
	if !foundRedis {
		t.Fatalf("expected redis component: %+v", got.Components)
	}
}
