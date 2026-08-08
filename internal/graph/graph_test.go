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

func TestBuildExternalNameAndEnvDependsOn(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "ext-db", Namespace: "prod"},
			Spec: corev1.ServiceSpec{
				Type:         corev1.ServiceTypeExternalName,
				ExternalName: "db.example.com",
			},
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "redis", Namespace: "prod"},
			Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "redis"}},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "api-0", Namespace: "prod"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:  "api",
					Image: "api",
					Env: []corev1.EnvVar{
						{Name: "DATABASE_URL", Value: "https://db.example.com:5432/app"},
						{Name: "REDIS_HOST", Value: "redis"},
						{Name: "SECRET_REF", ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "creds"},
								Key:                  "url",
							},
						}},
					},
				}},
			},
		},
	)
	rep, err := Build(context.Background(), client, Request{
		Namespace:           "prod",
		IncludeExternalDeps: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var extDep, svcDep bool
	for _, e := range rep.Edges {
		if e.Type != EdgeDependsOn {
			continue
		}
		if e.To == "ExternalHost/db.example.com" {
			extDep = true
			if strings.Contains(e.Detail, "https://") || strings.Contains(e.Detail, "5432/app") {
				t.Fatalf("must not store full URL in detail: %+v", e)
			}
		}
		if strings.Contains(e.To, "Service/redis") {
			svcDep = true
		}
	}
	if !extDep || !svcDep {
		t.Fatalf("expected ExternalName+env depends_on, edges=%+v", rep.Edges)
	}
	for _, n := range rep.Nodes {
		if n.Kind == NodeExternalHost && strings.Contains(n.Name, "https://") {
			t.Fatalf("node leaked URL: %+v", n)
		}
	}
}

func TestBuildReadyEndpointsOnly(t *testing.T) {
	ready := true
	notReady := false
	client := fake.NewSimpleClientset(
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "prod"},
			Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "api"}},
		},
		&discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "api-abc",
				Namespace: "prod",
				Labels:    map[string]string{discoveryv1.LabelServiceName: "api"},
			},
			Endpoints: []discoveryv1.Endpoint{
				{
					Conditions: discoveryv1.EndpointConditions{Ready: &ready},
					TargetRef:  &corev1.ObjectReference{Kind: "Pod", Name: "api-ready", Namespace: "prod"},
				},
				{
					Conditions: discoveryv1.EndpointConditions{Ready: &notReady},
					TargetRef:  &corev1.ObjectReference{Kind: "Pod", Name: "api-bad", Namespace: "prod"},
				},
			},
		},
	)
	rep, err := Build(context.Background(), client, Request{Namespace: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	var sawReady, sawBad bool
	for _, e := range rep.Edges {
		if e.Type != EdgeRoutes {
			continue
		}
		if strings.Contains(e.To, "api-ready") {
			sawReady = true
		}
		if strings.Contains(e.To, "api-bad") {
			sawBad = true
		}
	}
	if !sawReady || sawBad {
		t.Fatalf("ready=%v bad=%v edges=%+v", sawReady, sawBad, rep.Edges)
	}
}

func TestBuildNetworkPolicyPeerAllows(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "prod"},
			Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "db"}},
		},
		&networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: "api-egress", Namespace: "prod"},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
				Egress: []networkingv1.NetworkPolicyEgressRule{{
					To: []networkingv1.NetworkPolicyPeer{
						{PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "db"}}},
						{IPBlock: &networkingv1.IPBlock{CIDR: "10.0.0.0/8"}},
					},
				}},
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
	var toSvc, toCIDR bool
	for _, e := range rep.Edges {
		if e.Type != EdgeAllows {
			continue
		}
		if strings.Contains(e.To, "Service/db") {
			toSvc = true
		}
		if e.To == "ExternalHost/10.0.0.0/8" {
			toCIDR = true
		}
	}
	if !toSvc || !toCIDR {
		t.Fatalf("allows edges missing: %+v", rep.Edges)
	}
}

func TestImpactNotes(t *testing.T) {
	rep := Report{
		Nodes: []Node{
			{ID: "prod/Deployment/api", Kind: "Deployment", Name: "api", Namespace: "prod"},
			{ID: "prod/Pod/api-0", Kind: NodePod, Name: "api-0", Namespace: "prod"},
			{ID: "ExternalHost/db.example.com", Kind: NodeExternalHost, Name: "db.example.com"},
		},
		Edges: []Edge{
			{From: "prod/Pod/api-0", To: "ExternalHost/db.example.com", Type: EdgeDependsOn},
		},
	}
	note := ImpactNotes(rep, "prod", "Deployment", "api")
	if !strings.Contains(note, "db.example.com") {
		t.Fatalf("note=%q", note)
	}
}

func TestExtractHostname(t *testing.T) {
	cases := map[string]string{
		"https://db.example.com:5432/x": "db.example.com",
		"redis.prod.svc.cluster.local":  "redis.prod.svc.cluster.local",
		"redis":                         "redis",
		"/var/run":                      "",
		"":                              "",
	}
	for in, want := range cases {
		if got := ExtractHostname(in); got != want {
			t.Fatalf("%q: got %q want %q", in, got, want)
		}
	}
}



