package hpa

import (
	"strings"
	"testing"
)

func TestGenerate(t *testing.T) {
	manifest, summary, err := Generate(Request{
		TargetName:    "api",
		Namespace:     "prod",
		MinReplicas:   1,
		MaxReplicas:   5,
		CPUPercent:    70,
		MemoryPercent: 80,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(manifest, "kind: HorizontalPodAutoscaler") || !strings.Contains(manifest, "autoscaling/v2") {
		t.Fatalf("manifest=%s", manifest)
	}
	if !strings.Contains(manifest, "name: api-hpa") || !strings.Contains(manifest, "name: api") {
		t.Fatalf("manifest=%s", manifest)
	}
	if !strings.Contains(manifest, "cpu") || !strings.Contains(manifest, "memory") {
		t.Fatalf("manifest=%s", manifest)
	}
	if !strings.Contains(summary, "HPA/api-hpa") || !strings.Contains(summary, "cpu=70%") {
		t.Fatalf("summary=%s", summary)
	}
}

func TestGenerateRejectsMaxBelowMin(t *testing.T) {
	_, _, err := Generate(Request{TargetName: "api", MinReplicas: 5, MaxReplicas: 2, CPUPercent: 70})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDefaultHPAName(t *testing.T) {
	if got := DefaultHPAName("Redis API"); got != "redis-api-hpa" {
		t.Fatalf("got %q", got)
	}
}
