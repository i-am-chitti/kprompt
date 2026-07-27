package ctxbuild

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kprompt/kprompt/internal/incident"
	toolprometheus "github.com/kprompt/kprompt/internal/tools/prometheus"
	toolotel "github.com/kprompt/kprompt/internal/tools/otel"
)

func TestBuildSkipLiveReshapesEvidence(t *testing.T) {
	inc := incident.NewIncident("inc-1", "payments", time.Now().UTC())
	inc.Severity = incident.SeverityHigh
	inc.Summary = "BackOff on payment-api"
	inc.PrimaryResource = &incident.ResourceRef{Kind: "Deployment", Name: "payment-api", Namespace: "payments"}
	inc.Evidence = []incident.EvidenceRef{
		{Type: incident.EvidenceEvent, Reason: "BackOff", Message: "restarting"},
		{Type: incident.EvidenceLog, Message: "connection refused"},
	}
	b := &Builder{}
	got := b.Build(context.Background(), inc, Options{SkipLive: true})
	if got.Kind != Kind || len(got.LogSnippets) != 1 || len(got.RecentEvents) != 1 {
		t.Fatalf("%+v", got)
	}
	blocks := got.PromptBlocks()
	joined := strings.Join(blocks, "\n")
	if !strings.Contains(joined, "payment-api") || !strings.Contains(joined, "logs:") {
		t.Fatalf("prompt blocks:\n%s", joined)
	}
}

func TestBuildEnrichesDeploymentAndPod(t *testing.T) {
	replicas := int32(2)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "payment-api",
			Namespace: "payments",
			Annotations: map[string]string{
				"kubernetes.io/change-cause": "bump redis url",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "payment-api"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "payment-api"}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "api",
						Image: "payment:1",
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("256Mi"),
							},
						},
					}},
				},
			},
		},
		Status: appsv1.DeploymentStatus{ReadyReplicas: 1, UpdatedReplicas: 2, ObservedGeneration: 3},
	}
	dep.Generation = 3
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "payment-api-aaaaa-bbbbb",
			Namespace: "payments",
			Labels:    map[string]string{"app": "payment-api"},
		},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
			Containers: []corev1.Container{{
				Name:  "api",
				Image: "payment:1",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("256Mi")},
				},
			}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:         "api",
				Ready:        false,
				RestartCount: 7,
				State: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
				},
			}},
		},
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "payment-config", Namespace: "payments"},
	}
	client := fake.NewSimpleClientset(dep, pod, cm)
	b := &Builder{Client: client}

	inc := incident.NewIncident("inc-9", "payments", time.Now().UTC())
	inc.PrimaryResource = &incident.ResourceRef{Kind: "Deployment", Name: "payment-api", Namespace: "payments"}
	inc.Evidence = []incident.EvidenceRef{{
		Type:     incident.EvidenceObject,
		Resource: &incident.ResourceRef{Kind: "Pod", Name: "payment-api-aaaaa-bbbbb", Namespace: "payments"},
	}}

	got := b.Build(context.Background(), inc, Options{})
	if got.Deployment == nil || got.Deployment.ChangeCause != "bump redis url" {
		t.Fatalf("deployment: %+v", got.Deployment)
	}
	if got.Pod == nil || got.Pod.Name != "payment-api-aaaaa-bbbbb" {
		t.Fatalf("pod: %+v", got.Pod)
	}
	if len(got.Pod.Containers) == 0 || got.Pod.Containers[0].RestartCount != 7 {
		t.Fatalf("containers: %+v", got.Pod.Containers)
	}
	if len(got.ConfigMaps) == 0 {
		t.Fatal("expected configmap touch")
	}
}

func TestBuildDegradesWithoutClient(t *testing.T) {
	inc := incident.NewIncident("inc-2", "ns", time.Now().UTC())
	got := (&Builder{}).Build(context.Background(), inc, Options{})
	if len(got.Degraded) == 0 || got.Degraded[0] != "kubernetes" {
		t.Fatalf("degraded=%v", got.Degraded)
	}
}

type stubMetrics struct {
	val float64
	err error
}

func (s stubMetrics) Query(ctx context.Context, promQL string, at time.Time) (toolprometheus.Result, error) {
	if s.err != nil {
		return toolprometheus.Result{}, s.err
	}
	return toolprometheus.Result{
		Scalar: &toolprometheus.Sample{Value: fmt.Sprintf("%g", s.val)},
	}, nil
}

func TestEnrichMetricsSuccess(t *testing.T) {
	client := fake.NewSimpleClientset()
	b := &Builder{Client: client, Metrics: stubMetrics{val: 0.42}}
	inc := incident.NewIncident("inc-m", "payments", time.Now().UTC())
	inc.PrimaryResource = &incident.ResourceRef{Kind: "Deployment", Name: "payment-api", Namespace: "payments"}
	got := b.Build(context.Background(), inc, Options{})
	if len(got.Metrics) == 0 {
		t.Fatalf("expected metrics, degraded=%v", got.Degraded)
	}
	for _, d := range got.Degraded {
		if d == "prometheus" {
			t.Fatalf("should not degrade prometheus when metrics present: %v", got.Degraded)
		}
	}
	blocks := got.PromptBlocks()
	joined := strings.Join(blocks, "\n")
	if !strings.Contains(joined, "metrics:") {
		t.Fatalf("prompt missing metrics: %s", joined)
	}
}

func TestEnrichMetricsDegradesWhenMissing(t *testing.T) {
	client := fake.NewSimpleClientset()
	b := &Builder{Client: client} // no Metrics
	inc := incident.NewIncident("inc-m2", "payments", time.Now().UTC())
	inc.PrimaryResource = &incident.ResourceRef{Kind: "Deployment", Name: "api", Namespace: "payments"}
	got := b.Build(context.Background(), inc, Options{SkipTraces: true})
	found := false
	for _, d := range got.Degraded {
		if d == "prometheus" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected prometheus degraded, got %v", got.Degraded)
	}
}

type stubTraces struct {
	traces []toolotel.Trace
	err    error
}

func (s stubTraces) SearchTraces(ctx context.Context, req toolotel.SearchRequest) ([]toolotel.Trace, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.traces, nil
}

func TestEnrichTracesSuccess(t *testing.T) {
	client := fake.NewSimpleClientset()
	tr := toolotel.Trace{
		TraceID:   "abc123",
		Duration:  250 * time.Millisecond,
		StartTime: time.Now().UTC(),
		Spans: []toolotel.Span{{
			TraceID: "abc123", SpanID: "s1", Operation: "GET /charge",
			Duration: 250 * time.Millisecond, Status: "ERROR",
		}},
	}
	b := &Builder{
		Client:  client,
		Traces:  stubTraces{traces: []toolotel.Trace{tr}},
		Metrics: stubMetrics{val: 1}, // avoid prometheus degrade noise
	}
	inc := incident.NewIncident("inc-t", "payments", time.Now().UTC())
	inc.PrimaryResource = &incident.ResourceRef{Kind: "Deployment", Name: "payment-api", Namespace: "payments"}
	got := b.Build(context.Background(), inc, Options{})
	if len(got.Traces) == 0 {
		t.Fatalf("expected traces, degraded=%v", got.Degraded)
	}
}

func TestEnrichTracesDegradesWhenMissing(t *testing.T) {
	client := fake.NewSimpleClientset()
	b := &Builder{Client: client, Metrics: stubMetrics{val: 1}}
	inc := incident.NewIncident("inc-t2", "payments", time.Now().UTC())
	inc.PrimaryResource = &incident.ResourceRef{Kind: "Deployment", Name: "api", Namespace: "payments"}
	got := b.Build(context.Background(), inc, Options{SkipMetrics: true})
	found := false
	for _, d := range got.Degraded {
		if d == "otel" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected otel degraded, got %v", got.Degraded)
	}
}
