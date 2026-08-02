// Package watchassist is the opt-in laptop proactive scanner (S-014 · ADR-0022).
//
// It reads Pods/Events (and never mutates). Suggestions point operators at
// investigate/why prompts. Distinct from in-cluster Observe (ADR-0013).
package watchassist

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/kprompt/kprompt/internal/cluster"
)

const TypeWatchReport = "WatchReport"

// Request scopes one watch scan.
type Request struct {
	Namespace string // required for MVP (namespace-scoped)
	Prompt    string
	Now       time.Time
	EventAge  time.Duration // default 15m
}

// Suggestion is a non-mutating next-step hint.
type Suggestion struct {
	Severity string `json:"severity"` // low | medium | high
	Code     string `json:"code"`
	Title    string `json:"title"`
	Message  string `json:"message"`
	Command  string `json:"command,omitempty"` // suggested kprompt prompt
}

// Report is one scan result.
type Report struct {
	Type        string       `json:"type"`
	Namespace   string       `json:"namespace"`
	Summary     string       `json:"summary"`
	ScannedAt   time.Time    `json:"scannedAt"`
	Suggestions []Suggestion `json:"suggestions"`
	Degraded    []string     `json:"degraded,omitempty"`
}

// Analyzer performs a single read-only scan.
type Analyzer struct {
	Client kubernetes.Interface
}

// Run scans Pods + Warning Events and emits suggestions.
func (a *Analyzer) Run(ctx context.Context, req Request) (Report, error) {
	if a == nil || a.Client == nil {
		return Report{}, fmt.Errorf("watch: client required")
	}
	ns := strings.TrimSpace(req.Namespace)
	if ns == "" {
		return Report{}, fmt.Errorf("watch: namespace required (-n); laptop watch is namespace-scoped")
	}
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	age := req.EventAge
	if age <= 0 {
		age = 15 * time.Minute
	}

	out := Report{
		Type:      TypeWatchReport,
		Namespace: ns,
		ScannedAt: now,
	}
	opts := metav1.ListOptions{Limit: cluster.DefaultReadLimit}

	pods, err := a.Client.CoreV1().Pods(ns).List(ctx, opts)
	if err != nil {
		return Report{}, fmt.Errorf("list pods: %w", err)
	}
	for i := range pods.Items {
		out.Suggestions = append(out.Suggestions, podSuggestions(&pods.Items[i])...)
	}

	evs, err := a.Client.CoreV1().Events(ns).List(ctx, opts)
	if err != nil {
		out.Degraded = append(out.Degraded, "events: "+err.Error())
	} else {
		cutoff := now.Add(-age)
		for i := range evs.Items {
			ev := &evs.Items[i]
			if ev.Type != corev1.EventTypeWarning {
				continue
			}
			t := eventTime(ev)
			if !t.IsZero() && t.Before(cutoff) {
				continue
			}
			out.Suggestions = append(out.Suggestions, eventSuggestion(ev)...)
		}
	}

	out.Suggestions = dedupeSuggestions(out.Suggestions)
	sort.Slice(out.Suggestions, func(i, j int) bool {
		if out.Suggestions[i].Severity != out.Suggestions[j].Severity {
			return sevRank(out.Suggestions[i].Severity) > sevRank(out.Suggestions[j].Severity)
		}
		return out.Suggestions[i].Title < out.Suggestions[j].Title
	})
	out.Summary = fmt.Sprintf(
		"%d suggestion(s) in namespace %s (read-only; never auto-applies)",
		len(out.Suggestions), ns,
	)
	if len(out.Suggestions) == 0 {
		out.Summary = fmt.Sprintf("No proactive signals in namespace %s (Pods + recent Warning Events)", ns)
	}
	return out, nil
}

func podSuggestions(p *corev1.Pod) []Suggestion {
	var out []Suggestion
	name := p.Name
	ns := p.Namespace
	for _, cs := range p.Status.ContainerStatuses {
		if cs.State.Waiting != nil {
			reason := cs.State.Waiting.Reason
			switch reason {
			case "CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull", "CreateContainerConfigError":
				out = append(out, Suggestion{
					Severity: "high",
					Code:     "Watch.PodWaiting." + reason,
					Title:    fmt.Sprintf("Pod/%s waiting: %s", name, reason),
					Message:  cs.State.Waiting.Message,
					Command:  fmt.Sprintf(`kprompt "investigate %s" -n %s`, name, ns),
				})
			}
		}
		if cs.LastTerminationState.Terminated != nil &&
			cs.LastTerminationState.Terminated.Reason == "OOMKilled" {
			out = append(out, Suggestion{
				Severity: "high",
				Code:     "Watch.PodOOM",
				Title:    fmt.Sprintf("Pod/%s was OOMKilled", name),
				Message:  "Last termination reason OOMKilled",
				Command:  fmt.Sprintf(`kprompt "why is %s OOM" -n %s`, name, ns),
			})
		}
		if cs.RestartCount >= 5 {
			out = append(out, Suggestion{
				Severity: "medium",
				Code:     "Watch.PodRestarts",
				Title:    fmt.Sprintf("Pod/%s restart count %d", name, cs.RestartCount),
				Message:  "Elevated restarts — consider investigate",
				Command:  fmt.Sprintf(`kprompt "investigate %s" -n %s`, name, ns),
			})
		}
	}
	if p.Status.Phase == corev1.PodPending {
		out = append(out, Suggestion{
			Severity: "medium",
			Code:     "Watch.PodPending",
			Title:    fmt.Sprintf("Pod/%s is Pending", name),
			Message:  "Pending pods often need why/investigate",
			Command:  fmt.Sprintf(`kprompt "why is %s pending" -n %s`, name, ns),
		})
	}
	return out
}

func eventSuggestion(ev *corev1.Event) []Suggestion {
	involved := ev.InvolvedObject.Name
	if involved == "" {
		return nil
	}
	kind := ev.InvolvedObject.Kind
	ns := ev.Namespace
	code := "Watch.Event." + ev.Reason
	sev := "medium"
	switch ev.Reason {
	case "FailedScheduling", "FailedMount", "FailedCreatePodSandBox", "Unhealthy", "BackOff":
		sev = "high"
	}
	cmd := fmt.Sprintf(`kprompt "investigate %s" -n %s`, involved, ns)
	if strings.EqualFold(kind, "Pod") {
		cmd = fmt.Sprintf(`kprompt "why is %s failing" -n %s`, involved, ns)
	}
	return []Suggestion{{
		Severity: sev,
		Code:     code,
		Title:    fmt.Sprintf("%s/%s: %s", kind, involved, ev.Reason),
		Message:  strings.TrimSpace(ev.Message),
		Command:  cmd,
	}}
}

func eventTime(ev *corev1.Event) time.Time {
	if !ev.LastTimestamp.Time.IsZero() {
		return ev.LastTimestamp.Time
	}
	if !ev.EventTime.Time.IsZero() {
		return ev.EventTime.Time
	}
	return ev.CreationTimestamp.Time
}

func dedupeSuggestions(in []Suggestion) []Suggestion {
	seen := map[string]struct{}{}
	out := make([]Suggestion, 0, len(in))
	for _, s := range in {
		key := s.Code + "|" + s.Title
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, s)
	}
	return out
}

func sevRank(s string) int {
	switch strings.ToLower(s) {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}
