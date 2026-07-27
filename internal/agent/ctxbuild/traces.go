package ctxbuild

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kprompt/kprompt/internal/incident"
	toolotel "github.com/kprompt/kprompt/internal/tools/otel"
)

// TracesQuerier is the optional OTel/Jaeger/Tempo adapter (AG-025).
type TracesQuerier interface {
	SearchTraces(ctx context.Context, req toolotel.SearchRequest) ([]toolotel.Trace, error)
}

// enrichTraces attaches compact trace EvidenceRefs when a Querier is configured.
// Missing / failed backend → Degraded "otel" — never invents spans (ADR-0016).
func (b *Builder) enrichTraces(ctx context.Context, out *AgentContext, workload string) {
	if b == nil || b.Traces == nil {
		out.Degraded = appendUnique(out.Degraded, "otel")
		return
	}
	wl := strings.TrimSpace(workload)
	if wl == "" {
		out.Degraded = appendUnique(out.Degraded, "otel")
		return
	}

	now := time.Now().UTC()
	req := toolotel.SearchRequest{
		Service: wl,
		Start:   now.Add(-1 * time.Hour),
		End:     now,
		Limit:   5,
	}
	traces, err := b.Traces.SearchTraces(ctx, req)
	if err != nil {
		out.Degraded = appendUnique(out.Degraded, "otel")
		return
	}
	if len(traces) == 0 {
		out.Degraded = appendUnique(out.Degraded, "otel")
		return
	}

	trace, err := toolotel.LatestTrace(ctx, b.Traces, req)
	if err != nil {
		if errors.Is(err, toolotel.ErrTraceNotFound) {
			out.Degraded = appendUnique(out.Degraded, "otel")
			return
		}
		// Fall back to first search hit.
		trace = traces[0]
	}

	report := toolotel.AnalyzeTrace(trace)
	ts := now
	msg := strings.TrimSpace(report.Summary)
	if msg == "" {
		msg = fmt.Sprintf("trace %s duration=%s spans=%d",
			trace.TraceID, trace.Duration.Round(time.Millisecond), len(trace.Spans))
	}
	reason := "latest_trace"
	if len(report.Bottlenecks) > 0 {
		reason = "bottleneck"
		b0 := report.Bottlenecks[0]
		msg = fmt.Sprintf("%s; top=%s %s (%s)", msg, b0.Operation, b0.Duration.Round(time.Millisecond), b0.Status)
	}
	out.Traces = append(out.Traces, incident.EvidenceRef{
		Type:      incident.EvidenceTrace,
		Reason:    reason,
		Message:   truncate(msg, 400),
		Timestamp: &ts,
		Source:    "otel",
		URI:       trace.TraceID,
		Resource: &incident.ResourceRef{
			Kind:      "Workload",
			Name:      wl,
			Namespace: out.Namespace,
		},
	})
	for i, bn := range report.Bottlenecks {
		if i >= 2 {
			break
		}
		if !strings.EqualFold(bn.Status, "ERROR") {
			continue
		}
		out.Traces = append(out.Traces, incident.EvidenceRef{
			Type:      incident.EvidenceTrace,
			Reason:    "error_span",
			Message:   truncate(bn.Message, 300),
			Timestamp: &ts,
			Source:    "otel",
			URI:       trace.TraceID + "#" + bn.SpanID,
			Resource: &incident.ResourceRef{
				Kind:      "Workload",
				Name:      wl,
				Namespace: out.Namespace,
			},
		})
	}
}
