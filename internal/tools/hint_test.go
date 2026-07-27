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
		{IDHelm, []string{"Helm", "helm.sh"}},
		{IDPrometheus, []string{"Prometheus", "KPROMPT_PROMETHEUS_URL"}},
		{IDGrafana, []string{"Grafana", "KPROMPT_GRAFANA_URL"}},
		{IDOpenTelemetry, []string{"Trace", "KPROMPT_OTEL_ENDPOINT"}},
		{IDKubernetes, []string{"Kubernetes", "kubeconfig"}},
		{ID("unknown-tool"), []string{"not available"}},
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
