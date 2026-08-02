// Package score builds a cluster/namespace scorecard (S-011).
//
// Rollup of audit (security) + optimize inventory/idle/rightsizing/HPA
// (reliability + cost). Cost is skipped without Prometheus — no fake precision.
package score

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"k8s.io/client-go/kubernetes"

	"github.com/kprompt/kprompt/internal/audit"
	"github.com/kprompt/kprompt/internal/incident"
	"github.com/kprompt/kprompt/internal/optimize"
	toolprometheus "github.com/kprompt/kprompt/internal/tools/prometheus"
)

const (
	TypeScorecard = "Scorecard"

	DimSecurity    = "security"
	DimReliability = "reliability"
	DimCost        = "cost"

	StatusReady    = "ready"
	StatusDegraded = "degraded"
	StatusSkipped  = "skipped"
)

// Request scopes a scorecard.
type Request struct {
	Namespace string // empty = cluster-wide
	Prompt    string
	Window    time.Duration
}

// Evidence links a score deduction to a concrete finding.
type Evidence struct {
	Source    string `json:"source"` // audit | optimize
	Code      string `json:"code"`
	Severity  string `json:"severity,omitempty"`
	Title     string `json:"title,omitempty"`
	Message   string `json:"message,omitempty"`
	Resource  string `json:"resource,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Impact    int    `json:"impact"` // points deducted (positive)
}

// Dimension is one score axis (0–100 when ready).
type Dimension struct {
	ID       string     `json:"id"`
	Score    *int       `json:"score,omitempty"`
	Status   string     `json:"status"`
	Message  string     `json:"message"`
	Evidence []Evidence `json:"evidence,omitempty"`
}

// Report is the typed scorecard payload.
type Report struct {
	Type           string      `json:"type"`
	Scope          string      `json:"scope"`
	Namespace      string      `json:"namespace,omitempty"`
	ClusterContext string      `json:"cluster_context,omitempty"`
	Window         string      `json:"window,omitempty"`
	Overall        *int        `json:"overall,omitempty"`
	Verdict        string      `json:"verdict,omitempty"` // excellent | good | fair | poor | incomplete
	Summary        string      `json:"summary"`
	Dimensions     []Dimension `json:"dimensions"`
	Degraded       []string    `json:"degraded,omitempty"`
}

// Analyzer builds a scorecard from live cluster signals.
type Analyzer struct {
	Client     kubernetes.Interface
	Prometheus toolprometheus.Querier // optional
}

// Run collects audit + optimize signals and scores them.
func (a *Analyzer) Run(ctx context.Context, req Request) (Report, error) {
	if a == nil || a.Client == nil {
		return Report{}, fmt.Errorf("score: client required")
	}
	window := req.Window
	if window <= 0 {
		window = time.Hour
	}
	ns := strings.TrimSpace(req.Namespace)

	optReq := optimize.Request{Namespace: ns, Window: window}
	opt := optimize.BuildScaffold(optReq)
	inv, err := optimize.CollectInventory(ctx, a.Client, optReq)
	if err != nil {
		return Report{}, fmt.Errorf("score inventory: %w", err)
	}
	optimize.ApplyInventory(&opt, inv)
	idle := optimize.DetectIdle(ctx, a.Prometheus, opt.Workloads, window)
	optimize.ApplyIdle(&opt, idle)
	rs := optimize.SuggestRightsizing(ctx, a.Prometheus, opt.Workloads, window)
	optimize.ApplyRightsizing(&opt, rs)
	optimize.ApplyCostNotes(&opt, window)
	hpa := optimize.CollectHPAHints(ctx, a.Client, a.Prometheus, opt.Workloads, ns)
	optimize.ApplyHPA(&opt, hpa)

	aud, err := (&audit.Analyzer{Client: a.Client}).Run(ctx, audit.Request{
		Namespace: ns,
		Prompt:    req.Prompt,
	})
	if err != nil {
		return Report{}, fmt.Errorf("score audit: %w", err)
	}

	rep := FromSignals(opt, aud, ns)
	return rep, nil
}

// FromSignals computes the scorecard from collected optimize + audit artifacts.
func FromSignals(opt optimize.Report, aud incident.Investigation, ns string) Report {
	sec := scoreSecurity(aud)
	rel := scoreReliability(opt)
	cost := scoreCost(opt)

	dims := []Dimension{sec, rel, cost}
	var (
		sum   int
		count int
		deg   []string
	)
	for _, d := range aud.Degraded {
		deg = appendUnique(deg, "audit:"+d)
	}
	for _, d := range dims {
		if d.Status == StatusSkipped || d.Status == StatusDegraded {
			if d.Message != "" {
				deg = appendUnique(deg, d.ID+": "+d.Message)
			}
		}
		if d.Score != nil && d.Status != StatusSkipped {
			sum += *d.Score
			count++
		}
	}
	sort.Strings(deg)

	scope := optimize.ScopeCluster
	ns = strings.TrimSpace(ns)
	if ns != "" {
		scope = optimize.ScopeNamespace
	}

	out := Report{
		Type:       TypeScorecard,
		Scope:      scope,
		Namespace:  ns,
		Window:     opt.Window,
		Dimensions: dims,
		Degraded:   deg,
	}
	if count == 0 {
		out.Verdict = "incomplete"
		out.Summary = "Scorecard incomplete — no scorable dimensions (check RBAC / signals)"
		return out
	}
	overall := sum / count
	out.Overall = &overall
	out.Verdict = verdictFor(overall, count < 3)
	scopeLabel := "cluster"
	if scope == optimize.ScopeNamespace {
		scopeLabel = "namespace " + ns
	}
	out.Summary = fmt.Sprintf(
		"Scorecard %s: overall %d/100 (%s) — security %s, reliability %s, cost %s",
		scopeLabel, overall, out.Verdict,
		dimLabel(sec), dimLabel(rel), dimLabel(cost),
	)
	return out
}

func scoreSecurity(aud incident.Investigation) Dimension {
	score := 100
	var evidence []Evidence
	for _, f := range aud.Findings {
		if !strings.HasPrefix(f.Code, "Audit.") {
			continue
		}
		impact := severityImpact(f.Severity)
		if impact <= 0 {
			continue
		}
		score -= impact
		res := ""
		if len(f.Evidence) > 0 && f.Evidence[0].Resource != nil {
			r := f.Evidence[0].Resource
			res = r.Kind + "/" + r.Name
		}
		evidence = append(evidence, Evidence{
			Source:    "audit",
			Code:      f.Code,
			Severity:  f.Severity,
			Title:     f.Title,
			Message:   f.Message,
			Resource:  res,
			Namespace: f.Namespace,
			Impact:    impact,
		})
	}
	if score < 0 {
		score = 0
	}
	sortEvidence(evidence)
	msg := fmt.Sprintf("%d hygiene finding(s)", len(evidence))
	if len(evidence) == 0 {
		msg = "no hygiene findings matched MVP rules"
	}
	return Dimension{
		ID:       DimSecurity,
		Score:    intPtr(score),
		Status:   StatusReady,
		Message:  msg,
		Evidence: evidence,
	}
}

func scoreReliability(opt optimize.Report) Dimension {
	score := 100
	var evidence []Evidence

	notReady := 0
	for _, w := range opt.Workloads {
		if w.Replicas > 0 && w.ReadyReplicas < w.Replicas {
			notReady++
			impact := 8
			if w.ReadyReplicas == 0 {
				impact = 15
			}
			score -= impact
			evidence = append(evidence, Evidence{
				Source:    "optimize",
				Code:      "optimize.reliability.not_ready",
				Severity:  "high",
				Title:     "Workload not fully ready",
				Message:   fmt.Sprintf("%s/%s ready %d/%d", w.Kind, w.Name, w.ReadyReplicas, w.Replicas),
				Resource:  w.Kind + "/" + w.Name,
				Namespace: w.Namespace,
				Impact:    impact,
			})
		}
	}

	for _, f := range opt.Findings {
		switch f.Code {
		case "optimize.inventory.missing_requests":
			impact := 6
			score -= impact
			evidence = append(evidence, Evidence{
				Source: "optimize", Code: f.Code, Severity: f.Severity,
				Title: f.Title, Message: f.Message, Resource: f.Resource, Namespace: f.Namespace, Impact: impact,
			})
		case "optimize.inventory.missing_limits":
			impact := 3
			score -= impact
			evidence = append(evidence, Evidence{
				Source: "optimize", Code: f.Code, Severity: f.Severity,
				Title: f.Title, Message: f.Message, Resource: f.Resource, Namespace: f.Namespace, Impact: impact,
			})
		case "optimize.hpa.maxed", "optimize.hpa.static":
			impact := 5
			score -= impact
			evidence = append(evidence, Evidence{
				Source: "optimize", Code: f.Code, Severity: f.Severity,
				Title: f.Title, Message: f.Message, Resource: f.Resource, Namespace: f.Namespace, Impact: impact,
			})
		}
	}

	status := StatusReady
	msg := fmt.Sprintf("%d workload(s) not fully ready", notReady)
	if notReady == 0 && len(evidence) == 0 {
		msg = "workloads look ready; no reliability deductions"
	} else if notReady == 0 {
		msg = fmt.Sprintf("%d reliability signal(s)", len(evidence))
	}
	if opt.Sections.HPA.Status == optimize.SectionSkipped {
		status = StatusDegraded
		msg += "; HPA section skipped"
	}

	if score < 0 {
		score = 0
	}
	sortEvidence(evidence)
	return Dimension{
		ID:       DimReliability,
		Score:    intPtr(score),
		Status:   status,
		Message:  msg,
		Evidence: evidence,
	}
}

func scoreCost(opt optimize.Report) Dimension {
	if opt.Sections.Idle.Status == optimize.SectionSkipped ||
		opt.Sections.Idle.Status == optimize.SectionPending {
		msg := opt.Sections.Idle.Message
		if msg == "" {
			msg = "Prometheus idle/rightsizing unavailable — cost score omitted (no fake precision)"
		}
		return Dimension{
			ID:      DimCost,
			Status:  StatusSkipped,
			Message: msg,
		}
	}

	score := 100
	var evidence []Evidence
	idleN := 0
	for _, f := range opt.Findings {
		switch f.Code {
		case "optimize.idle.workload":
			idleN++
			impact := 8
			if strings.EqualFold(f.Severity, "high") {
				impact = 12
			}
			score -= impact
			evidence = append(evidence, Evidence{
				Source: "optimize", Code: f.Code, Severity: f.Severity,
				Title: f.Title, Message: f.Message, Resource: f.Resource, Namespace: f.Namespace, Impact: impact,
			})
		case "optimize.rightsizing.delta":
			// Only count lower suggestions as cost waste.
			if !strings.Contains(strings.ToLower(f.Message), "lower") &&
				!strings.Contains(strings.ToLower(f.Title), "lower") {
				continue
			}
			impact := 5
			score -= impact
			evidence = append(evidence, Evidence{
				Source: "optimize", Code: f.Code, Severity: f.Severity,
				Title: f.Title, Message: f.Message, Resource: f.Resource, Namespace: f.Namespace, Impact: impact,
			})
		case "optimize.cost.notes":
			evidence = append(evidence, Evidence{
				Source: "optimize", Code: f.Code, Severity: f.Severity,
				Title: f.Title, Message: f.Message, Impact: 0,
			})
		}
	}
	if score < 0 {
		score = 0
	}
	sortEvidence(evidence)
	msg := fmt.Sprintf("%d idle/underutilized signal(s)", idleN)
	if idleN == 0 && len(evidence) == 0 {
		msg = "no idle/rightsizing cost pressure detected"
	}
	return Dimension{
		ID:       DimCost,
		Score:    intPtr(score),
		Status:   StatusReady,
		Message:  msg,
		Evidence: evidence,
	}
}

func severityImpact(sev string) int {
	switch strings.ToLower(strings.TrimSpace(sev)) {
	case incident.SeverityCritical:
		return 25
	case incident.SeverityHigh:
		return 12
	case incident.SeverityMedium:
		return 5
	case incident.SeverityLow:
		return 2
	default:
		return 0
	}
}

func verdictFor(overall int, incomplete bool) string {
	if incomplete {
		switch {
		case overall >= 85:
			return "good"
		case overall >= 70:
			return "fair"
		default:
			return "poor"
		}
	}
	switch {
	case overall >= 90:
		return "excellent"
	case overall >= 75:
		return "good"
	case overall >= 55:
		return "fair"
	default:
		return "poor"
	}
}

func dimLabel(d Dimension) string {
	if d.Status == StatusSkipped || d.Score == nil {
		return "n/a"
	}
	return fmt.Sprintf("%d", *d.Score)
}

func intPtr(v int) *int { return &v }

func appendUnique(in []string, s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return in
	}
	for _, x := range in {
		if x == s {
			return in
		}
	}
	return append(in, s)
}

func sortEvidence(ev []Evidence) {
	sort.Slice(ev, func(i, j int) bool {
		if ev[i].Impact != ev[j].Impact {
			return ev[i].Impact > ev[j].Impact
		}
		if ev[i].Code != ev[j].Code {
			return ev[i].Code < ev[j].Code
		}
		return ev[i].Resource < ev[j].Resource
	})
}
