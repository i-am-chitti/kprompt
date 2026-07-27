package ctxbuild

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/kprompt/kprompt/internal/incident"
	toolprometheus "github.com/kprompt/kprompt/internal/tools/prometheus"
)

// MetricsQuerier is the optional Prometheus adapter used by enrichMetrics (AG-024).
type MetricsQuerier interface {
	Query(ctx context.Context, promQL string, at time.Time) (toolprometheus.Result, error)
}

// enrichMetrics attaches metric EvidenceRefs when a Querier is configured.
// Missing / failed Prom → Degraded "prometheus" — never invents values (ADR-0016).
func (b *Builder) enrichMetrics(ctx context.Context, out *AgentContext, workload string) {
	if b == nil || b.Metrics == nil {
		out.Degraded = appendUnique(out.Degraded, "prometheus")
		return
	}
	ns := out.Namespace
	wl := strings.TrimSpace(workload)
	if wl == "" {
		out.Degraded = appendUnique(out.Degraded, "prometheus")
		return
	}
	podRE := "^" + regexp.QuoteMeta(wl) + "-.*"

	specs := []struct {
		reason string
		unit   string
		query  string
	}{
		{
			reason: "cpu_usage",
			unit:   "cores",
			query: fmt.Sprintf(
				`sum(rate(container_cpu_usage_seconds_total{namespace=%q,pod=~%q,container!="",container!="POD"}[5m]))`,
				ns, podRE,
			),
		},
		{
			reason: "memory_working_set",
			unit:   "bytes",
			query: fmt.Sprintf(
				`sum(container_memory_working_set_bytes{namespace=%q,pod=~%q,container!="",container!="POD"})`,
				ns, podRE,
			),
		},
		{
			reason: "restart_rate",
			unit:   "restarts/s",
			query: fmt.Sprintf(
				`sum(rate(kube_pod_container_status_restarts_total{namespace=%q,pod=~%q}[15m]))`,
				ns, podRE,
			),
		},
	}

	gotAny := false
	var lastErr error
	now := time.Now().UTC()
	for _, spec := range specs {
		res, err := b.Metrics.Query(ctx, spec.query, time.Time{})
		if err != nil {
			lastErr = err
			continue
		}
		val, ok, err := toolprometheus.FirstValue(res)
		if err != nil || !ok {
			if err != nil {
				lastErr = err
			}
			continue
		}
		gotAny = true
		ts := now
		msg := fmt.Sprintf("%.4g %s", val, spec.unit)
		out.Metrics = append(out.Metrics, incident.EvidenceRef{
			Type:      incident.EvidenceMetric,
			Reason:    spec.reason,
			Message:   msg,
			Timestamp: &ts,
			Source:    "prometheus",
			URI:       spec.query,
			Resource: &incident.ResourceRef{
				Kind:      "Workload",
				Name:      wl,
				Namespace: ns,
			},
		})
	}
	if !gotAny {
		out.Degraded = appendUnique(out.Degraded, "prometheus")
		if lastErr != nil {
			// Keep message out of invented metrics; degrade only.
			_ = lastErr
		}
	}
}
