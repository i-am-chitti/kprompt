// Package confidence calibrates Namespace Agent analysis scores (AG-029 / ADR-0016).
//
// Memory and patterns may boost confidence elsewhere; this package only adjusts
// based on evidence richness and never invents cluster facts.
package confidence

import (
	"strings"

	"github.com/kprompt/kprompt/internal/agent/ctxbuild"
)

// Adjust applies multi-signal calibration to a base confidence in [0,1].
// When llmTrusted is true, harsh “not enough evidence” caps are skipped (LLM already reasoned).
// Returns adjusted confidence and an optional note.
func Adjust(base float64, agentCtx ctxbuild.AgentContext, detectorMatched bool, llmTrusted bool) (float64, string) {
	conf := clamp(base)
	note := ""

	evidenceN := len(agentCtx.Incident.Evidence) + len(agentCtx.RecentEvents) + len(agentCtx.LogSnippets) + len(agentCtx.Metrics) + len(agentCtx.Traces) + len(agentCtx.GitOps)
	if !llmTrusted {
		switch {
		case evidenceN == 0:
			conf = min(conf, 0.35)
			note = "not enough evidence"
		case evidenceN == 1 && !detectorMatched:
			conf = min(conf, 0.5)
			note = "weak evidence"
		case evidenceN >= 4 && detectorMatched:
			conf = min(conf+0.05, 0.98)
		}
		if !detectorMatched && strings.TrimSpace(agentCtx.Incident.RootCause) == "" {
			note = firstNonEmpty(note, "not enough evidence")
			conf = min(conf, 0.45)
		}
	} else if evidenceN >= 4 {
		conf = min(conf+0.02, 0.98)
	}

	if len(agentCtx.Metrics) > 0 {
		conf = min(conf+0.03, 0.98)
	}
	if len(agentCtx.Traces) > 0 {
		conf = min(conf+0.03, 0.98)
	}
	if len(agentCtx.GitOps) > 0 {
		conf = min(conf+0.02, 0.98)
	}
	// AG-034: memory is evidence-not-proof — never raise confidence from memory alone.
	if len(agentCtx.Memory) > 0 && evidenceN == 0 {
		conf = min(conf, 0.35)
		note = firstNonEmpty(note, "memory is not proof")
	}
	if len(agentCtx.Degraded) > 0 {
		conf = max(conf-0.08*float64(minInt(len(agentCtx.Degraded), 3)), 0.2)
	}
	return clamp(conf), note
}

// NotEnoughEvidenceRoot is the honest root-cause string when signals are weak.
const NotEnoughEvidenceRoot = "I don't have enough evidence"

func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
