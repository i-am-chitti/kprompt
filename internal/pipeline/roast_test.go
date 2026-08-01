package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kprompt/kprompt/internal/config"
	"github.com/kprompt/kprompt/internal/output"
)

func TestRoastRunsWithoutLLM(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "payments"}},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "api-1", Namespace: "payments"},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{
					Ready: true, RestartCount: 0,
				}},
			},
		},
	)
	var out bytes.Buffer
	err := RunWith(context.Background(), config.Resolved{
		Namespace:        "payments",
		NamespaceFromCLI: true,
		Prompt:           "how's my namespace",
	}, &out, Deps{
		Client: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "Cluster roast") || !strings.Contains(got, "payments") {
		t.Fatalf("unexpected roast output:\n%s", got)
	}
	if !strings.Contains(got, "Score:") {
		t.Fatalf("expected score line:\n%s", got)
	}
}

func TestRoastJSONResult(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
	)
	var out bytes.Buffer
	err := RunWith(context.Background(), config.Resolved{
		Prompt: "how's my cluster",
		Output: "json",
	}, &out, Deps{
		Client: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc output.PlanResult
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if doc.Plan.Intent != "roast" {
		t.Fatalf("intent=%q doc=%+v", doc.Plan.Intent, doc)
	}
	if !doc.Applied {
		t.Fatal("expected applied=true for read-only roast")
	}
	if !bytes.Contains(doc.Result, []byte(`"type":"ClusterRoast"`)) {
		t.Fatalf("result=%s", string(doc.Result))
	}
}
