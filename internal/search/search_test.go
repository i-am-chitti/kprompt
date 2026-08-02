package search

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestSearchFindsDeploymentUsingRedis(t *testing.T) {
	client := fake.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "cache", Namespace: "payments"},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:  "redis",
							Image: "redis:7-alpine",
						}},
					},
				},
			},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "payments"},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:  "api",
							Image: "ghcr.io/acme/api:1.2",
							Env: []corev1.EnvVar{
								{Name: "CACHE_URL", Value: "redis://cache:6379"},
							},
						}},
					},
				},
			},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "payments"},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:  "nginx",
							Image: "nginx:1.27",
						}},
					},
				},
			},
		},
	)

	got, err := (&Analyzer{Client: client}).Run(context.Background(), Request{
		Namespace: "payments",
		Prompt:    "find every Deployment using redis",
		Query:     "redis",
		Kind:      "Deployment",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != TypeSearch {
		t.Fatalf("type=%q", got.Type)
	}
	if !strings.Contains(got.Summary, "2 hit") && !strings.Contains(got.Summary, "3 hit") {
		// image hit on cache + env hit on api (possibly more if name matches)
		t.Fatalf("summary=%q hits=%d", got.Summary, len(got.Hits))
	}
	if !hasHit(got, "Deployment", "cache", "image") {
		t.Fatalf("expected cache image hit: %+v", got.Hits)
	}
	if !hasHit(got, "Deployment", "api", "env") {
		t.Fatalf("expected api env hit: %+v", got.Hits)
	}
	if hasHit(got, "Deployment", "web", "image") {
		t.Fatal("web should not match redis")
	}
}

func TestSearchRequiresQuery(t *testing.T) {
	client := fake.NewSimpleClientset()
	_, err := (&Analyzer{Client: client}).Run(context.Background(), Request{
		Namespace: "payments",
		Query:     "",
	})
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestSearchLabelAndService(t *testing.T) {
	client := fake.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "worker",
				Namespace: "shop",
				Labels:    map[string]string{"app": "redis-worker"},
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "w", Image: "busybox:1"}},
					},
				},
			},
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "redis", Namespace: "shop"},
			Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "redis"}},
		},
	)
	got, err := (&Analyzer{Client: client}).Run(context.Background(), Request{
		Namespace: "shop",
		Query:     "redis",
		Kind:      "Service",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasHit(got, "Service", "redis", "name") {
		t.Fatalf("expected service name hit: %+v", got.Hits)
	}

	got2, err := (&Analyzer{Client: client}).Run(context.Background(), Request{
		Namespace: "shop",
		Query:     "redis",
		Kind:      "Deployment",
		Match:     "label",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasHit(got2, "Deployment", "worker", "label") {
		t.Fatalf("expected label hit: %+v", got2.Hits)
	}
}

func hasHit(r Report, kind, name, field string) bool {
	for _, h := range r.Hits {
		if h.Kind == kind && h.Name == name && h.Field == field {
			return true
		}
	}
	return false
}
