package learn

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kprompt/kprompt/internal/tools"
)

func TestFromRegistryAndPromptBias(t *testing.T) {
	reg := tools.NewRegistry([]tools.Result{
		{ID: tools.IDKubernetes, Name: "Kubernetes", Status: tools.StatusAvailable, Detail: "context: kind-dev"},
		{ID: tools.IDHelm, Name: "Helm", Status: tools.StatusAvailable, Detail: "/usr/local/bin/helm"},
		{ID: tools.IDGatewayAPI, Name: "Gateway API", Status: tools.StatusAvailable, Detail: "Gateway CRD present"},
		{ID: tools.IDIstio, Name: "Istio", Status: tools.StatusUnavailable, Detail: "missing"},
	})
	p := FromRegistry("kind-dev", reg)
	if p.Context != "kind-dev" || p.Version != Version {
		t.Fatalf("profile meta: %+v", p)
	}
	if len(p.Available) != 2 {
		t.Fatalf("available = %v", p.Available)
	}
	bias := p.PromptBias()
	if bias == "" || !strings.Contains(bias, "Helm") || !strings.Contains(bias, "Gateway API") || !strings.Contains(bias, "prefer") {
		t.Fatalf("bias = %q", bias)
	}
	sum := p.Summary()
	if !strings.Contains(sum, "kind-dev") || !strings.Contains(sum, "Helm") || !strings.Contains(sum, "Gateway API") {
		t.Fatalf("summary = %q", sum)
	}
}

func TestRunPersists(t *testing.T) {
	store := NewMemStore()
	reg := tools.NewRegistry([]tools.Result{
		{ID: tools.IDKubernetes, Name: "Kubernetes", Status: tools.StatusAvailable, Detail: "context: c1"},
		{ID: tools.IDPrometheus, Name: "Prometheus", Status: tools.StatusConfigured, Detail: "http://prom"},
	})
	p, err := Run(context.Background(), Options{
		Context: "c1",
		Store:   store,
		Detect: func(context.Context, tools.DetectOptions) (*tools.Registry, error) {
			return reg, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !p.LearnedAt.After(time.Time{}) {
		t.Fatal("expected learned_at")
	}
	loaded, ok, err := store.Load("c1")
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	if len(loaded.Available) != 1 || loaded.Available[0] != string(tools.IDPrometheus) {
		t.Fatalf("loaded available = %v", loaded.Available)
	}
}

func TestFileStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := FileStore{Dir: dir}
	p := Profile{
		Version:   Version,
		Context:   "docker-desktop",
		LearnedAt: time.Now().UTC().Truncate(time.Second),
		Tools: []ToolEntry{
			{ID: "helm", Name: "Helm", Status: "available", Available: true},
		},
		Available: []string{"helm"},
	}
	if err := store.Save(p); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.Load("docker-desktop")
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	if got.Context != p.Context || len(got.Available) != 1 {
		t.Fatalf("got = %+v", got)
	}
}
