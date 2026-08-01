package graph

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func int32PtrLocal(v int32) *int32 { return &v }

func TestBuildServiceGraphRoutesAndNetworkPolicy(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "prod"},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{"app": "api"},
			},
		},
		&discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "api-abc",
				Namespace: "prod",
				Labels:    map[string]string{discoveryv1.LabelServiceName: "api"},
			},
			Ports: []discoveryv1.EndpointPort{{Port: int32PtrLocal(8080)}},
			Endpoints: []discoveryv1.Endpoint{{
				TargetRef: &corev1.ObjectReference{Kind: "Pod", Name: "api-1", Namespace: "prod"},
			}},
		},
		&networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: "allow-api", Namespace: "prod"},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
			},
		},
	)

	rep, err := Build(context.Background(), client, Request{
		Namespace:            "prod",
		IncludeNetworkPolicy: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Scope != ScopeNamespace || rep.Type != "service-graph" {
		t.Fatalf("%+v", rep)
	}
	if len(rep.Nodes) < 3 {
		t.Fatalf("nodes=%+v", rep.Nodes)
	}
	var hasRoute, hasSelects bool
	for _, e := range rep.Edges {
		if e.Type == EdgeRoutes && e.Source == SourceKubernetes {
			hasRoute = true
		}
		if e.Type == EdgeSelects {
			hasSelects = true
		}
	}
	if !hasRoute || !hasSelects {
		t.Fatalf("edges=%+v", rep.Edges)
	}
}

func TestBuildIngressAndPVC(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
			Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "web"}},
		},
		&networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{Name: "web-ing", Namespace: "prod"},
			Spec: networkingv1.IngressSpec{
				Rules: []networkingv1.IngressRule{{
					Host: "web.example.com",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{{
								Path: "/",
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{Name: "web"},
								},
							}},
						},
					},
				}},
			},
		},
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "prod"},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "web-0", Namespace: "prod"},
			Spec: corev1.PodSpec{
				Volumes: []corev1.Volume{{
					Name: "data",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"},
					},
				}},
				Containers: []corev1.Container{{Name: "c", Image: "nginx"}},
			},
		},
	)

	rep, err := Build(context.Background(), client, Request{
		Namespace:      "prod",
		IncludeIngress: true,
		IncludePVC:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var hasExpose, hasMount bool
	for _, e := range rep.Edges {
		if e.Type == EdgeExposes {
			hasExpose = true
		}
		if e.Type == EdgeMounts {
			hasMount = true
		}
	}
	if !hasExpose || !hasMount {
		t.Fatalf("edges=%+v summary=%s", rep.Edges, rep.Summary)
	}
	if !strings.Contains(rep.Summary, "ingresses") || !strings.Contains(rep.Summary, "PVCs") {
		t.Fatalf("summary=%q", rep.Summary)
	}
}

func TestBuildSecretConfigMapRefs(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "prod"},
			Spec: corev1.PodSpec{
				Volumes: []corev1.Volume{
					{
						Name:   "tls",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{SecretName: "api-tls"},
						},
					},
					{
						Name: "cfg",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: "api-cfg"},
							},
						},
					},
				},
				Containers: []corev1.Container{{
					Name:  "c",
					Image: "api",
					Env: []corev1.EnvVar{{
						Name: "TOKEN",
						ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "api-token"},
								Key:                  "token",
							},
						},
					}},
				}},
			},
		},
	)
	rep, err := Build(context.Background(), client, Request{
		Namespace:         "prod",
		IncludeVolumeRefs: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, e := range rep.Edges {
		if e.Type != EdgeMounts {
			continue
		}
		for _, n := range rep.Nodes {
			if n.ID == e.To {
				kinds[n.Kind] = true
			}
		}
	}
	if !kinds[NodeSecret] || !kinds[NodeConfigMap] {
		t.Fatalf("kinds=%v edges=%+v", kinds, rep.Edges)
	}
	for _, n := range rep.Notes {
		if strings.Contains(n, "never Secret.data") {
			return
		}
	}
	t.Fatalf("expected honesty note, notes=%v", rep.Notes)
}

func TestBuildRequiresClient(t *testing.T) {
	_, err := Build(context.Background(), nil, Request{})
	if err == nil {
		t.Fatal("expected error")
	}
}


