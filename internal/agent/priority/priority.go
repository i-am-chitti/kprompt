// Package priority implements ADR-0016 §5 objective ranking for Namespace Agent (AG-030).
//
// Ranking order (highest first): outage → data loss → security → availability →
// reliability → performance → cost → best practices.
package priority

import (
	"strings"

	"github.com/kprompt/kprompt/internal/agent/ctxbuild"
	"github.com/kprompt/kprompt/internal/incident"
)

// Objective identifiers (stable for reports / tests).
const (
	ObjectiveOutage        = "outage"
	ObjectiveDataLoss      = "data_loss"
	ObjectiveSecurity      = "security"
	ObjectiveAvailability  = "availability"
	ObjectiveReliability   = "reliability"
	ObjectivePerformance   = "performance"
	ObjectiveCost          = "cost"
	ObjectiveBestPractices = "best_practices"
)

// Classification is the ADR-0016 objective for an analysis result.
type Classification struct {
	Objective string // Objective*
	Rank      int    // 1 = highest priority … 8 = lowest
	Reason    string
}

// RankOf returns 1..8 for a known objective (0 if unknown).
func RankOf(objective string) int {
	switch strings.ToLower(strings.TrimSpace(objective)) {
	case ObjectiveOutage:
		return 1
	case ObjectiveDataLoss:
		return 2
	case ObjectiveSecurity:
		return 3
	case ObjectiveAvailability:
		return 4
	case ObjectiveReliability:
		return 5
	case ObjectivePerformance:
		return 6
	case ObjectiveCost:
		return 7
	case ObjectiveBestPractices:
		return 8
	default:
		return 0
	}
}

// Classify maps detector / context signals onto an ADR-0016 objective.
func Classify(agentCtx ctxbuild.AgentContext, detectorCode, severity, summary, root string) Classification {
	blob := strings.ToLower(strings.Join([]string{
		detectorCode, severity, summary, root,
		agentCtx.Incident.Summary, agentCtx.Incident.RootCause,
	}, " "))
	for _, e := range agentCtx.Incident.Evidence {
		blob += " " + strings.ToLower(e.Reason+" "+e.Message)
	}
	for _, e := range agentCtx.RecentEvents {
		blob += " " + strings.ToLower(e.Reason+" "+e.Message)
	}

	code := strings.ToLower(strings.TrimSpace(detectorCode))
	switch {
	case code == "oom.killed" || strings.Contains(blob, "oom") ||
		strings.Contains(blob, "outage") || strings.Contains(blob, "sev-1") ||
		strings.Contains(blob, "page now"):
		return Classification{Objective: ObjectiveOutage, Rank: 1, Reason: "production outage signal"}
	case strings.Contains(blob, "data loss") || strings.Contains(blob, "volume corrupt") ||
		(strings.Contains(blob, "pvc") && strings.Contains(blob, "lost")):
		return Classification{Objective: ObjectiveDataLoss, Rank: 2, Reason: "possible data loss"}
	case strings.Contains(blob, "rbac") || strings.Contains(blob, "unauthorized") ||
		strings.Contains(blob, "forbidden") || strings.Contains(blob, "security") ||
		strings.Contains(blob, "cve"):
		return Classification{Objective: ObjectiveSecurity, Rank: 3, Reason: "security-related signal"}
	case code == "crashloop" || code == "image.pull" || code == "probe.fail" ||
		code == "schedule.pending" || strings.Contains(blob, "crashloop") ||
		strings.Contains(blob, "unavailable") || strings.Contains(blob, "not ready"):
		return Classification{Objective: ObjectiveAvailability, Rank: 4, Reason: "availability impact"}
	case code == "rollout.failed" || code == "storage.pvc" || strings.Contains(blob, "restart") ||
		strings.Contains(blob, "backoff"):
		return Classification{Objective: ObjectiveReliability, Rank: 5, Reason: "reliability / flaky behavior"}
	case code == "dns.fail" || strings.Contains(blob, "latency") || strings.Contains(blob, "timeout") ||
		strings.Contains(blob, "slow") || strings.Contains(blob, "cpu_usage"):
		return Classification{Objective: ObjectivePerformance, Rank: 6, Reason: "performance signal"}
	case strings.Contains(blob, "cost") || strings.Contains(blob, "rightsiz") ||
		strings.Contains(blob, "over-provision"):
		return Classification{Objective: ObjectiveCost, Rank: 7, Reason: "cost / rightsizing"}
	}

	switch strings.ToLower(strings.TrimSpace(severity)) {
	case incident.SeverityCritical:
		return Classification{Objective: ObjectiveOutage, Rank: 1, Reason: "critical severity"}
	case incident.SeverityHigh:
		return Classification{Objective: ObjectiveAvailability, Rank: 4, Reason: "high severity"}
	case incident.SeverityMedium:
		return Classification{Objective: ObjectiveReliability, Rank: 5, Reason: "medium severity"}
	case incident.SeverityLow:
		return Classification{Objective: ObjectivePerformance, Rank: 6, Reason: "low severity"}
	default:
		return Classification{Objective: ObjectiveBestPractices, Rank: 8, Reason: "default / hygiene"}
	}
}

// SeverityFloor is the minimum severity for an objective (ADR-0016 ranking).
func SeverityFloor(objective string) string {
	switch RankOf(objective) {
	case 1, 2: // outage, data loss
		return incident.SeverityCritical
	case 3, 4: // security, availability
		return incident.SeverityHigh
	case 5: // reliability
		return incident.SeverityMedium
	case 6: // performance
		return incident.SeverityLow
	case 7, 8: // cost, best practices
		return incident.SeverityInfo
	default:
		return ""
	}
}

// ApplySeverity raises severity to at least the objective floor (never lowers).
func ApplySeverity(current, objective string) string {
	floor := SeverityFloor(objective)
	if floor == "" {
		return current
	}
	if severityRank(current) < severityRank(floor) {
		return floor
	}
	return current
}

// Less reports whether a has higher priority than b (for sorting).
func Less(a, b Classification) bool {
	if a.Rank == 0 {
		return false
	}
	if b.Rank == 0 {
		return true
	}
	return a.Rank < b.Rank
}

// SortActions orders RecommendedActions so higher ADR objectives come first.
// Uses ActionID / Title keywords when Objective is not stamped on the action.
func SortActions(actions []incident.RecommendedAction) {
	if len(actions) < 2 {
		return
	}
	// Stable insertion by inferred rank.
	for i := 1; i < len(actions); i++ {
		j := i
		for j > 0 && actionRank(actions[j]) < actionRank(actions[j-1]) {
			actions[j], actions[j-1] = actions[j-1], actions[j]
			j--
		}
	}
}

func actionRank(a incident.RecommendedAction) int {
	blob := strings.ToLower(a.ActionID + " " + a.Title + " " + a.Why + " " + a.Risk)
	c := Classify(ctxbuild.AgentContext{}, "", "", blob, blob)
	if c.Rank == 0 {
		return 8
	}
	return c.Rank
}

func severityRank(s string) int {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case incident.SeverityCritical:
		return 5
	case incident.SeverityHigh:
		return 4
	case incident.SeverityMedium:
		return 3
	case incident.SeverityLow:
		return 2
	case incident.SeverityInfo:
		return 1
	default:
		return 0
	}
}
