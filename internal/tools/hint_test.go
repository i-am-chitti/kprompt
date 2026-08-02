package tools

import (
	"strings"
	"testing"
)

func TestMissingHint(t *testing.T) {
	cases := []struct {
		id   ID
		subs []string
	}{
		{IDHelm, []string{"Helm", "kprompt setup", "minimal"}},
		{IDPrometheus, []string{"Prometheus", "kprompt setup", "prometheus"}},
		{IDGrafana, []string{"Grafana", "kprompt setup", "grafana"}},
		{IDOpenTelemetry, []string{"Trace", "kprompt setup", "opentelemetry"}},
		{IDKubernetes, []string{"Kubernetes", "kubeconfig", "does not create clusters"}},
		{ID("unknown-tool"), []string{"not available"}},
		{IDArgoWorkflows, []string{"Argo", "kprompt setup", "argo-workflows"}},
	}
	for _, tc := range cases {
		t.Run(string(tc.id), func(t *testing.T) {
			got := MissingHint(tc.id)
			if got == "" {
				t.Fatal("empty hint")
			}
			for _, sub := range tc.subs {
				if !strings.Contains(got, sub) {
					t.Fatalf("hint %q missing %q", got, sub)
				}
			}
		})
	}
}
