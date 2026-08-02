package score

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kprompt/kprompt/internal/incident"
	"github.com/kprompt/kprompt/internal/optimize"
)

func TestFromSignalsSecurityAndSkippedCost(t *testing.T) {
	opt := optimize.BuildScaffold(optimize.Request{Namespace: "payments"})
	optimize.ApplyInventory(&opt, optimize.Inventory{
		Workloads: []optimize.Workload{{
			Kind: "Deployment", Namespace: "payments", Name: "api",
			Replicas: 2, ReadyReplicas: 2,
		}},
	})
	// Force idle skipped (no Prom).
	optimize.ApplyIdle(&opt, optimize.IdleResult{Skipped: true, SkipReason: "Prometheus not configured"})

	aud := incident.NewInvestigation("score payments", "payments")
	aud.Findings = []incident.Finding{{
		Code:      "Audit.Privileged",
		Severity:  incident.SeverityCritical,
		Title:     "privileged",
		Namespace: "payments",
		Evidence: []incident.EvidenceRef{{
			Resource: &incident.ResourceRef{Kind: "Deployment", Name: "bad", Namespace: "payments"},
		}},
	}}

	got := FromSignals(opt, aud, "payments")
	if got.Type != TypeScorecard {
		t.Fatalf("type=%q", got.Type)
	}
	if got.Overall == nil {
		t.Fatal("expected overall")
	}
	sec := dim(got, DimSecurity)
	if sec.Score == nil || *sec.Score != 75 { // 100-25
		t.Fatalf("security=%v", sec)
	}
	cost := dim(got, DimCost)
	if cost.Status != StatusSkipped || cost.Score != nil {
		t.Fatalf("cost should be skipped without Prom: %+v", cost)
	}
	if len(sec.Evidence) != 1 || sec.Evidence[0].Code != "Audit.Privileged" {
		t.Fatalf("evidence=%+v", sec.Evidence)
	}
}

func TestScoreReliabilityNotReady(t *testing.T) {
	opt := optimize.Report{
		Type: "optimize",
		Sections: optimize.Sections{
			Idle: optimize.SectionStatus{Status: optimize.SectionSkipped, Message: "no prom"},
			HPA:  optimize.SectionStatus{Status: optimize.SectionReady},
		},
		Workloads: []optimize.Workload{{
			Kind: "Deployment", Namespace: "ns", Name: "web",
			Replicas: 3, ReadyReplicas: 1,
		}},
	}
	got := FromSignals(opt, incident.NewInvestigation("score", "ns"), "ns")
	rel := dim(got, DimReliability)
	if rel.Score == nil || *rel.Score != 92 { // 100-8
		t.Fatalf("reliability=%v evidence=%+v", rel, rel.Evidence)
	}
}

func TestAnalyzerRun(t *testing.T) {
	priv := true
	replicas := int32(1)
	client := fake.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "bad", Namespace: "payments"},
			Spec: appsv1.DeploymentSpec{
				Replicas: &replicas,
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:  "app",
							Image: "nginx:latest",
							SecurityContext: &corev1.SecurityContext{Privileged: &priv},
						}},
					},
				},
			},
			Status: appsv1.DeploymentStatus{Replicas: 1, ReadyReplicas: 1},
		},
	)
	got, err := (&Analyzer{Client: client}).Run(context.Background(), Request{
		Namespace: "payments",
		Prompt:    "score payments namespace",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Overall == nil {
		t.Fatal("expected overall score")
	}
	sec := dim(got, DimSecurity)
	if sec.Score == nil || *sec.Score >= 100 {
		t.Fatalf("expected security deductions: %+v", sec)
	}
	cost := dim(got, DimCost)
	if cost.Status != StatusSkipped {
		t.Fatalf("cost without Prom should skip: %+v", cost)
	}
}

func dim(r Report, id string) Dimension {
	for _, d := range r.Dimensions {
		if d.ID == id {
			return d
		}
	}
	return Dimension{}
}
