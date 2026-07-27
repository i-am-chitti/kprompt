package incident

import (
	"fmt"
	"strings"
	"time"
)

const (
	// SchemaVersion2 is used by Namespace Agent InvestigationReport (AG-022 / ADR-0016).
	// Incident / AgentAlert / Investigation remain on SchemaVersion "1".
	SchemaVersion2 = "2"

	KindInvestigationReport = "InvestigationReport"
)

// Hypothesis is one RCA candidate with an optional causal chain.
// Prefer CausalChain over a single symptom string (ADR-0016 §6).
type Hypothesis struct {
	// Statement is the probable root cause (not merely the symptom).
	Statement string `json:"statement"`
	// CausalChain is ordered from symptom → … → root (may be empty for alternatives).
	CausalChain []string `json:"causalChain,omitempty"`
	Confidence  float64  `json:"confidence"` // 0..1
	// Primary marks the leading hypothesis when multiple are listed.
	Primary bool `json:"primary,omitempty"`
}

// RecommendedAction is a ranked remediation proposal.
// Mutate-relevant actions stay PlanResult-shaped at apply time (ADR-0003 / ADR-0015).
type RecommendedAction struct {
	Title          string  `json:"title"`
	Why            string  `json:"why,omitempty"`
	Risk           string  `json:"risk,omitempty"`
	ExpectedImpact string  `json:"expectedImpact,omitempty"`
	Rollback       string  `json:"rollback,omitempty"`
	Confidence     float64 `json:"confidence,omitempty"` // 0..1
	// ActionID is an optional Autopilot allowlist id (e.g. rollbackFailedRollout).
	ActionID string `json:"actionId,omitempty"`
	// PlanHint is a short PlanResult-shaped hint; never auto-applied.
	PlanHint string `json:"planHint,omitempty"`
}

// InvestigationReport is the Namespace Agent outbound intelligence document (AG-022).
// Slack, webhook, and Coordinator handoff share this shape (ADR-0016 §7).
// It extends ADR-0014 without breaking Observe V1 Incident/AgentAlert consumers.
type InvestigationReport struct {
	APIVersion     string    `json:"apiVersion"`
	Kind           string    `json:"kind"`
	SchemaVersion  string    `json:"schemaVersion"`
	ID             string    `json:"id,omitempty"`
	IncidentID     string    `json:"incidentId,omitempty"`
	Namespace      string    `json:"namespace"`
	ClusterContext string    `json:"cluster_context,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`

	// Facts — concise observed state (not interpretation).
	Facts string `json:"facts,omitempty"`
	// Summary — short what-happened for humans / notifiers.
	Summary string `json:"summary"`
	// Reasoning — how evidence led to hypotheses (explainability).
	Reasoning string `json:"reasoning,omitempty"`

	Evidence   []EvidenceRef `json:"evidence,omitempty"`
	Timeline   []EvidenceRef `json:"timeline,omitempty"`
	Hypotheses []Hypothesis  `json:"hypotheses,omitempty"`

	Confidence float64 `json:"confidence"` // 0..1 overall (usually primary hypothesis)

	RecommendedActions []RecommendedAction `json:"recommendedActions,omitempty"`
	Risks              []string            `json:"risks,omitempty"`
	Unknowns           []string            `json:"unknowns,omitempty"`
	// Degraded lists missing signal backends (prometheus, otel, …).
	Degraded []string `json:"degraded,omitempty"`

	Severity string `json:"severity,omitempty"`
	// PrimaryResource is the main affected workload when known.
	PrimaryResource *ResourceRef  `json:"primaryResource,omitempty"`
	Affected        []ResourceRef `json:"affected,omitempty"`
}

// NewInvestigationReport returns a schema-stamped report shell (schemaVersion 2).
func NewInvestigationReport(namespace string, at time.Time) InvestigationReport {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return InvestigationReport{
		APIVersion:    APIVersion,
		Kind:          KindInvestigationReport,
		SchemaVersion: SchemaVersion2,
		Namespace:     strings.TrimSpace(namespace),
		CreatedAt:     at.UTC(),
		Hypotheses:    []Hypothesis{},
		Evidence:      []EvidenceRef{},
		Timeline:      []EvidenceRef{},
	}
}

// ReportFromIncident lifts an analyzed Incident into InvestigationReport v2.
// RootCause becomes the primary hypothesis; missing sections stay empty for the formatter to fill.
func ReportFromIncident(inc Incident, at time.Time) InvestigationReport {
	rep := NewInvestigationReport(inc.Namespace, at)
	rep.ID = strings.TrimSpace(inc.ID)
	rep.IncidentID = strings.TrimSpace(inc.ID)
	rep.ClusterContext = inc.ClusterContext
	rep.Summary = inc.Summary
	rep.Confidence = inc.Confidence
	rep.Severity = inc.Severity
	rep.PrimaryResource = inc.PrimaryResource
	rep.Affected = append([]ResourceRef(nil), inc.Affected...)
	rep.Evidence = append([]EvidenceRef(nil), inc.Evidence...)
	if rc := strings.TrimSpace(inc.RootCause); rc != "" {
		rep.Hypotheses = []Hypothesis{{
			Statement:  rc,
			Confidence: inc.Confidence,
			Primary:    true,
		}}
	}
	if rec := strings.TrimSpace(inc.Recommendation); rec != "" {
		rep.RecommendedActions = []RecommendedAction{{
			Title:      rec,
			Confidence: inc.Confidence,
		}}
	}
	return rep
}

// PrimaryHypothesis returns the primary hypothesis, or the first one, or nil.
func (r InvestigationReport) PrimaryHypothesis() *Hypothesis {
	for i := range r.Hypotheses {
		if r.Hypotheses[i].Primary {
			return &r.Hypotheses[i]
		}
	}
	if len(r.Hypotheses) > 0 {
		return &r.Hypotheses[0]
	}
	return nil
}

// RootCauseHint returns the primary hypothesis statement, or empty.
func (r InvestigationReport) RootCauseHint() string {
	if h := r.PrimaryHypothesis(); h != nil {
		return strings.TrimSpace(h.Statement)
	}
	return ""
}

// ValidateInvestigationReport checks Namespace Agent report shape before notify / handoff.
func ValidateInvestigationReport(r InvestigationReport) error {
	if strings.TrimSpace(r.APIVersion) == "" || strings.TrimSpace(r.Kind) == "" {
		return fmt.Errorf("investigationReport: apiVersion and kind are required")
	}
	if r.Kind != KindInvestigationReport {
		return fmt.Errorf("investigationReport: kind must be %s", KindInvestigationReport)
	}
	if strings.TrimSpace(r.SchemaVersion) == "" {
		return fmt.Errorf("investigationReport: schemaVersion is required")
	}
	if strings.TrimSpace(r.Namespace) == "" {
		return fmt.Errorf("investigationReport: namespace is required")
	}
	if strings.TrimSpace(r.Summary) == "" {
		return fmt.Errorf("investigationReport: summary is required")
	}
	if r.Confidence < 0 || r.Confidence > 1 {
		return fmt.Errorf("investigationReport: confidence must be in [0,1], got %v", r.Confidence)
	}
	if sev := strings.TrimSpace(r.Severity); sev != "" && !validSeverity(sev) {
		return fmt.Errorf("investigationReport: invalid severity %q", sev)
	}
	for i, h := range r.Hypotheses {
		if strings.TrimSpace(h.Statement) == "" {
			return fmt.Errorf("investigationReport: hypotheses[%d] statement is required", i)
		}
		if h.Confidence < 0 || h.Confidence > 1 {
			return fmt.Errorf("investigationReport: hypotheses[%d] confidence must be in [0,1]", i)
		}
	}
	for i, a := range r.RecommendedActions {
		if strings.TrimSpace(a.Title) == "" {
			return fmt.Errorf("investigationReport: recommendedActions[%d] title is required", i)
		}
		if a.Confidence < 0 || a.Confidence > 1 {
			return fmt.Errorf("investigationReport: recommendedActions[%d] confidence must be in [0,1]", i)
		}
	}
	return nil
}
