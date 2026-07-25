// Package analyze turns AgentContext into a gated AgentAlert (AG-008).
//
// Pipeline: context → (optional LLM structured completion) → confidence/severity gate → AgentAlert.
// One LLM call per incident evidence fingerprint — not per raw watch event.
package analyze

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kprompt/kprompt/internal/agent/ctxbuild"
	"github.com/kprompt/kprompt/internal/agent/patterns"
	"github.com/kprompt/kprompt/internal/incident"
	"github.com/kprompt/kprompt/internal/llm"
)

// Result from a structured LLM (or heuristic) analysis.
type Result struct {
	Severity       string  `json:"severity"`
	Confidence     float64 `json:"confidence"`
	Summary        string  `json:"summary"`
	RootCause      string  `json:"rootCause"`
	Recommendation string  `json:"recommendation"`
}

var analysisSchema = json.RawMessage(`{
  "type": "object",
  "required": ["severity", "confidence", "summary", "rootCause", "recommendation"],
  "properties": {
    "severity": {"type": "string", "enum": ["info", "low", "medium", "high", "critical"]},
    "confidence": {"type": "number", "minimum": 0, "maximum": 1},
    "summary": {"type": "string"},
    "rootCause": {"type": "string"},
    "recommendation": {"type": "string"}
  }
}`)

const systemPrompt = `You are kprompt Observe Mode, an AI SRE assistant for Kubernetes.
Given structured incident context, produce a concise root-cause analysis.
Rules:
- Use only evidence in the context; do not invent cluster facts.
- If signals are weak, lower confidence.
- recommendation must be safe guidance (check/verify); never claim you mutated the cluster.
- severity must be one of: info, low, medium, high, critical.
- Respond with JSON only matching the schema.`

// Options configures the analyzer gate.
type Options struct {
	MinSeverity   string
	MinConfidence float64
	// HeuristicOnly skips the LLM even when Provider is set (tests).
	HeuristicOnly bool
}

// Analyzer maps AgentContext → AgentAlert with dedupe + gate.
type Analyzer struct {
	Provider llm.Provider
	Options  Options
	// Patterns optional AG-016 library — boosts confidence on “seen before”; never mutates.
	Patterns *patterns.Library

	mu       sync.Mutex
	lastFP   map[string]string // incidentID → evidence fingerprint
	lastPass map[string]bool   // last gate result (for updated suppression noise)
}

// New returns an analyzer with Observe defaults.
func New(provider llm.Provider, opts Options) *Analyzer {
	if opts.MinSeverity == "" {
		opts.MinSeverity = incident.DefaultMinSeverity()
	}
	if opts.MinConfidence <= 0 {
		opts.MinConfidence = incident.DefaultMinConfidence()
	}
	return &Analyzer{
		Provider: provider,
		Options:  opts,
		lastFP:   map[string]string{},
		lastPass: map[string]bool{},
	}
}

// AnalyzeOutcome is returned to the agent loop.
type AnalyzeOutcome struct {
	Alert       incident.AgentAlert `json:"alert"`
	PassedGate  bool                `json:"passedGate"`
	Skipped     bool                `json:"skipped,omitempty"` // deduped LLM call
	Source      string              `json:"source"`            // llm | heuristic
	Result      Result              `json:"result"`
	SeenBefore  string              `json:"seenBefore,omitempty"` // AG-016 note
	PatternHits int                 `json:"patternHits,omitempty"`
}

// Analyze runs structured analysis for an open/updated incident context.
// alertStatus should be fired|updated|recovered.
func (a *Analyzer) Analyze(ctx context.Context, agentCtx ctxbuild.AgentContext, alertStatus string) (AnalyzeOutcome, error) {
	if a == nil {
		return AnalyzeOutcome{}, fmt.Errorf("analyze: analyzer is nil")
	}
	inc := agentCtx.Incident
	fp := fingerprint(inc)
	a.mu.Lock()
	prev, seen := a.lastFP[inc.ID]
	if seen && prev == fp {
		a.mu.Unlock()
		return AnalyzeOutcome{Skipped: true, Source: "dedupe"}, nil
	}
	a.lastFP[inc.ID] = fp
	a.mu.Unlock()

	var (
		res    Result
		source string
		err    error
	)
	useLLM := a.Provider != nil && !a.Options.HeuristicOnly
	if useLLM {
		res, err = a.callLLM(ctx, agentCtx)
		source = "llm"
		if err != nil {
			// Fall back to heuristic rather than failing the watch loop.
			res = Heuristic(agentCtx)
			source = "heuristic"
			err = nil
		}
	} else {
		res = Heuristic(agentCtx)
		source = "heuristic"
	}

	normalizeResult(&res, inc)

	recordRoot := res.RootCause
	recordRec := res.Recommendation

	var seenNote string
	var hits int
	if a.Patterns != nil {
		if match, ok := a.Patterns.Match(agentCtx.Namespace, agentCtx); ok {
			hits = match.Count
			boosted, note := patterns.ApplyBoost(patterns.SeverityConfidence{
				Confidence:     res.Confidence,
				RootCause:      res.RootCause,
				Recommendation: res.Recommendation,
			}, match)
			res.Confidence = boosted.Confidence
			res.RootCause = boosted.RootCause
			res.Recommendation = boosted.Recommendation
			seenNote = note
		}
	}

	// Enrich incident fields for NewAgentAlert
	inc.Severity = res.Severity
	inc.Confidence = res.Confidence
	inc.Summary = res.Summary
	inc.RootCause = res.RootCause
	inc.Recommendation = res.Recommendation

	if alertStatus == "" {
		alertStatus = incident.AlertFired
	}
	alert := incident.NewAgentAlert(inc, alertStatus, time.Now().UTC())
	alert.Affected = append([]incident.ResourceRef(nil), inc.Affected...)
	if len(alert.Evidence) == 0 {
		alert.Evidence = append([]incident.EvidenceRef(nil), agentCtx.LogSnippets...)
		alert.Evidence = append(alert.Evidence, agentCtx.RecentEvents...)
	}

	passed := incident.MeetsAlertGate(alert, a.Options.MinSeverity, a.Options.MinConfidence)
	if err := incident.ValidateAgentAlert(alert); err != nil && passed {
		passed = false
	}

	// Learn after analysis (Observe-only — never applies a mutate from the pattern).
	if a.Patterns != nil && alertStatus != incident.AlertRecovered {
		if _, rerr := a.Patterns.Record(agentCtx.Namespace, agentCtx, res.Severity, res.Summary, recordRoot, recordRec); rerr != nil {
			// Non-fatal: learning must not break the watch loop.
		}
	}

	a.mu.Lock()
	a.lastPass[inc.ID] = passed
	a.mu.Unlock()

	return AnalyzeOutcome{
		Alert:       alert,
		PassedGate:  passed,
		Source:      source,
		Result:      res,
		SeenBefore:  seenNote,
		PatternHits: hits,
	}, nil
}

func (a *Analyzer) callLLM(ctx context.Context, agentCtx ctxbuild.AgentContext) (Result, error) {
	user := strings.Join(agentCtx.PromptBlocks(), "\n")
	raw, err := a.Provider.CompleteStructured(ctx, llm.CompletionRequest{
		System: systemPrompt,
		User:   user,
	}, analysisSchema)
	if err != nil {
		return Result{}, err
	}
	var res Result
	if err := json.Unmarshal(raw, &res); err != nil {
		return Result{}, fmt.Errorf("analyze: decode LLM JSON: %w", err)
	}
	return res, nil
}

// Heuristic builds a Result without an LLM (offline / fallback).
func Heuristic(agentCtx ctxbuild.AgentContext) Result {
	inc := agentCtx.Incident
	sev := inc.Severity
	if sev == "" {
		sev = incident.SeverityMedium
	}
	summary := inc.Summary
	if summary == "" {
		summary = "Problem signal detected"
	}
	root := "Insufficient automated signal for a precise root cause"
	rec := "Inspect pod events and recent logs; verify dependent Services/Endpoints"
	conf := 0.55

	blob := strings.ToLower(strings.Join([]string{
		summary,
		joinEvidence(inc.Evidence),
		joinEvidence(agentCtx.LogSnippets),
		joinEvidence(agentCtx.RecentEvents),
		podSignals(agentCtx.Pod),
		deploymentSignals(agentCtx.Deployment),
	}, " "))
	switch {
	case strings.Contains(blob, "oom"):
		sev = incident.SeverityCritical
		root = "Likely memory limit exceeded (OOMKilled)"
		rec = "Raise memory limit/request or fix memory leak; check recent traffic/deploy"
		conf = 0.85
	// ImagePullBackOff Events often arrive as Reason=BackOff with a thin message;
	// pod waiting state / "pulling image" must win before the CrashLoop branch.
	case strings.Contains(blob, "imagepull"),
		strings.Contains(blob, "errimage"),
		strings.Contains(blob, "failed to pull"),
		strings.Contains(blob, "pulling image"),
		strings.Contains(blob, "manifest unknown"):
		sev = incident.SeverityHigh
		root = "Image pull failure"
		rec = "Verify image name/tag and registry credentials (imagePullSecrets)"
		conf = 0.85
	case strings.Contains(blob, "crashloop"),
		strings.Contains(blob, "backoff"):
		sev = incident.SeverityHigh
		root = "Container repeatedly crashing (CrashLoopBackOff)"
		rec = "Check previous container logs and readiness/liveness probes; verify config/secret refs"
		conf = 0.8
	case strings.Contains(blob, "failedscheduling"), strings.Contains(blob, "pending"):
		sev = incident.SeverityMedium
		root = "Pod cannot be scheduled"
		rec = "Check node resources, taints/tolerations, and PVC binding"
		conf = 0.75
	case strings.Contains(blob, "unhealthy"), strings.Contains(blob, "probe"):
		sev = incident.SeverityMedium
		root = "Probe failing / container marked unhealthy"
		rec = "Review probe paths/timeouts and application readiness"
		conf = 0.7
	}
	if agentCtx.Deployment != nil && agentCtx.Deployment.ChangeCause != "" {
		root = root + "; recent change-cause: " + agentCtx.Deployment.ChangeCause
		conf = minFloat(conf+0.05, 0.95)
	}
	if len(agentCtx.Degraded) > 0 {
		conf = maxFloat(conf-0.1, 0.3)
	}
	return Result{
		Severity:       sev,
		Confidence:     conf,
		Summary:        summary,
		RootCause:      root,
		Recommendation: rec,
	}
}

func podSignals(pod *ctxbuild.PodSnapshot) string {
	if pod == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(pod.Phase)
	b.WriteByte(' ')
	for _, c := range pod.Containers {
		b.WriteString(c.State)
		b.WriteByte(' ')
		b.WriteString(c.LastTermination)
		b.WriteByte(' ')
		b.WriteString(c.Image)
		b.WriteByte(' ')
	}
	return b.String()
}

func deploymentSignals(dep *ctxbuild.DeploymentSnapshot) string {
	if dep == nil {
		return ""
	}
	return dep.ChangeCause
}

func normalizeResult(res *Result, inc incident.Incident) {
	res.Severity = strings.ToLower(strings.TrimSpace(res.Severity))
	switch res.Severity {
	case incident.SeverityInfo, incident.SeverityLow, incident.SeverityMedium,
		incident.SeverityHigh, incident.SeverityCritical:
	default:
		if inc.Severity != "" {
			res.Severity = inc.Severity
		} else {
			res.Severity = incident.SeverityMedium
		}
	}
	if res.Confidence < 0 {
		res.Confidence = 0
	}
	if res.Confidence > 1 {
		res.Confidence = 1
	}
	if strings.TrimSpace(res.Summary) == "" {
		res.Summary = firstNonEmpty(inc.Summary, "Incident "+inc.ID)
	}
	if strings.TrimSpace(res.RootCause) == "" {
		res.RootCause = "Unknown"
	}
	if strings.TrimSpace(res.Recommendation) == "" {
		res.Recommendation = "Investigate with kprompt \"investigate <workload>\" or explain / kubectl describe"
	}
}

func fingerprint(inc incident.Incident) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s|%s|%d|", inc.ID, inc.Status, len(inc.Evidence))
	for _, e := range inc.Evidence {
		_, _ = fmt.Fprintf(h, "%s|%s|%s|", e.Type, e.Reason, e.Message)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func joinEvidence(ev []incident.EvidenceRef) string {
	var b strings.Builder
	for _, e := range ev {
		b.WriteString(e.Reason)
		b.WriteByte(' ')
		b.WriteString(e.Message)
		b.WriteByte(' ')
	}
	return b.String()
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// AlertStatusFor maps correlate change kinds to AgentAlert status.
func AlertStatusFor(changeKind string) string {
	switch changeKind {
	case "closed":
		return incident.AlertRecovered
	case "updated", "reopened":
		return incident.AlertUpdated
	default:
		return incident.AlertFired
	}
}
