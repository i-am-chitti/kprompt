// Package logs fetches on-demand pod log snippets for Observe incidents (AG-005).
//
// Triggered only by problem incident signals — never Follow-all-pods.
package logs

import (
	"context"
	"fmt"
	"strings"
	"time"

	"k8s.io/client-go/kubernetes"

	agentwatch "github.com/kprompt/kprompt/internal/agent/watch"
	"github.com/kprompt/kprompt/internal/cluster"
	"github.com/kprompt/kprompt/internal/incident"
)

const (
	DefaultTail     int64 = 300
	DefaultSince          = 5 * time.Minute
	DefaultMaxBytes       = 8 * 1024
)

// Fetcher pulls a short log window when an incident opens/updates.
type Fetcher struct {
	Client   kubernetes.Interface
	Tail     int64
	Since    time.Duration
	MaxBytes int
}

// New returns a fetcher with Observe defaults (300 lines / 5m / 8KiB cap).
func New(client kubernetes.Interface) *Fetcher {
	return &Fetcher{
		Client:   client,
		Tail:     DefaultTail,
		Since:    DefaultSince,
		MaxBytes: DefaultMaxBytes,
	}
}

// ShouldFetch is true for problem signals that warrant a log tail.
func ShouldFetch(ev agentwatch.Event) bool {
	if ev.Resource == agentwatch.ResourcePod {
		switch ev.PodPhase {
		case "Failed", "Pending", "Unknown":
			return true
		default:
			return false
		}
	}
	if ev.Resource != agentwatch.ResourceEvent {
		return false
	}
	r := strings.ToLower(ev.Reason)
	msg := strings.ToLower(ev.Message)
	switch {
	case strings.Contains(r, "backoff"), strings.Contains(r, "crash"),
		strings.Contains(r, "oom"), strings.Contains(r, "failed"),
		strings.Contains(r, "unhealthy"), strings.Contains(r, "probe"),
		strings.Contains(msg, "crashloop"), strings.Contains(msg, "oomkilled"):
		return true
	default:
		return false
	}
}

// preferPrevious uses the last terminated container logs for CrashLoop/OOM style signals.
func preferPrevious(ev agentwatch.Event) bool {
	r := strings.ToLower(ev.Reason + " " + ev.Message)
	return strings.Contains(r, "backoff") || strings.Contains(r, "crash") ||
		strings.Contains(r, "oom") || ev.PodPhase == "Failed"
}

// Attach fetches logs for the signal target and appends a truncated EvidenceRef on the incident.
// Failures are recorded as a short evidence note — they never abort correlation.
func (f *Fetcher) Attach(ctx context.Context, inc *incident.Incident, ev agentwatch.Event) {
	if f == nil || f.Client == nil || inc == nil || !ShouldFetch(ev) {
		return
	}
	res, err := f.Fetch(ctx, ev)
	if err != nil {
		now := time.Now().UTC()
		inc.Evidence = append(inc.Evidence, incident.EvidenceRef{
			Type:      incident.EvidenceLog,
			Reason:    "log-fetch-failed",
			Message:   err.Error(),
			Timestamp: &now,
			Source:    "agent",
		})
		return
	}
	now := time.Now().UTC()
	snip := res.Body
	msg := snip
	if res.Truncated {
		msg = snip + "\n…(truncated)"
	}
	uri := fmt.Sprintf("pod/%s?tail=%d", res.Pod, res.Tail)
	if res.Previous {
		uri += "&previous=1"
	}
	inc.Evidence = append(inc.Evidence, incident.EvidenceRef{
		Type: incident.EvidenceLog,
		Resource: &incident.ResourceRef{
			Kind:      "Pod",
			Name:      res.Pod,
			Namespace: res.Namespace,
		},
		Reason:    firstNonEmpty(ev.Reason, "logs"),
		Message:   msg,
		Timestamp: &now,
		Source:    "kubernetes",
		URI:       uri,
	})
}

// Result is a capped log snippet.
type Result struct {
	Pod       string
	Namespace string
	Container string
	Body      string
	Tail      int64
	Previous  bool
	Truncated bool
}

// Fetch resolves the pod from the watch event and returns a short log window.
func (f *Fetcher) Fetch(ctx context.Context, ev agentwatch.Event) (Result, error) {
	tail := f.Tail
	if tail <= 0 {
		tail = DefaultTail
	}
	since := f.Since
	if since <= 0 {
		since = DefaultSince
	}
	maxBytes := f.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}

	ns := ev.Namespace
	if ns == "" && ev.InvolvedName != "" {
		ns = "default"
	}
	kind, name := targetFromEvent(ev)
	if name == "" {
		return Result{}, fmt.Errorf("logs: no pod/workload target on event")
	}

	prev := preferPrevious(ev)
	reader := &cluster.LogReader{Client: f.Client}
	got, err := reader.Logs(ctx, cluster.LogsRequest{
		Name:         name,
		Namespace:    ns,
		Kind:         kind,
		Tail:         tail,
		Previous:     prev,
		SinceSeconds: int64(since.Seconds()),
	})
	if err != nil && prev {
		// Previous container may not exist yet — retry current.
		got, err = reader.Logs(ctx, cluster.LogsRequest{
			Name:         name,
			Namespace:    ns,
			Kind:         kind,
			Tail:         tail,
			SinceSeconds: int64(since.Seconds()),
		})
		prev = false
	}
	if err != nil {
		return Result{}, err
	}

	body := got.Body
	truncated := false
	if len(body) > maxBytes {
		body = body[len(body)-maxBytes:]
		truncated = true
	}
	return Result{
		Pod:       got.Pod,
		Namespace: got.Namespace,
		Container: got.Container,
		Body:      body,
		Tail:      got.Tail,
		Previous:  prev,
		Truncated: truncated,
	}, nil
}

func targetFromEvent(ev agentwatch.Event) (kind, name string) {
	if ev.Resource == agentwatch.ResourceEvent && strings.TrimSpace(ev.InvolvedName) != "" {
		kind = ev.InvolvedKind
		if kind == "" {
			kind = "Pod"
		}
		return kind, ev.InvolvedName
	}
	if ev.Resource == agentwatch.ResourcePod && ev.Name != "" {
		return "Pod", ev.Name
	}
	return "", ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
