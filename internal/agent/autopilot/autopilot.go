// Package autopilot implements policy-gated Autopilot proposals (AG-017 · ADR-0015).
//
// MVP is propose-only by default: PlanResult-shaped proposals + audit.
// Apply requires policy allowlist + Apply=true + confidence floor.
// Actions outside the allowlist are hard-denied. Never expands the allowlist via LLM.
package autopilot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kprompt/kprompt/internal/agent/ctxbuild"
	"github.com/kprompt/kprompt/internal/incident"
)

const (
	APIVersion    = "kprompt.io/v1"
	KindProposal  = "AutopilotProposal"
	SchemaVersion = "1"

	// MVP allowlist (ADR-0015).
	ActionRollbackFailedRollout = "rollbackFailedRollout"
	ActionRestartDeployment     = "restartDeployment" // deny in MVP apply; may propose later

	DecisionProposed = "proposed"
	DecisionApproved = "approved"
	DecisionDenied   = "denied"
	DecisionApplied  = "applied"
	DecisionFailed   = "failed"

	DefaultMinConfidence = 0.85
)

// MVPAllowlist is the only action IDs Autopilot may ever propose/apply in V1.
var MVPAllowlist = []string{ActionRollbackFailedRollout}

// Policy is the per-namespace Autopilot gate (ADR-0015 §4).
type Policy struct {
	// Allow lists action IDs permitted in this namespace (subset of MVPAllowlist).
	Allow []string `json:"allow"`
	// Apply enables policyAuto. Default false = proposeOnly.
	Apply bool `json:"apply"`
	// MinConfidence required before propose/apply (default 0.85).
	MinConfidence float64 `json:"minConfidence"`
}

// Proposal is a PlanResult-shaped Autopilot artifact (auditable; Applied false unless gated).
type Proposal struct {
	APIVersion    string    `json:"apiVersion"`
	Kind          string    `json:"kind"`
	SchemaVersion string    `json:"schemaVersion"`
	ID            string    `json:"id"`
	Namespace     string    `json:"namespace"`
	ActionID      string    `json:"actionId"`
	Decision      string    `json:"decision"` // proposed|approved|denied|applied|failed
	Reason        string    `json:"reason,omitempty"`
	Confidence    float64   `json:"confidence"`
	IncidentID    string    `json:"incidentId,omitempty"`
	TargetKind    string    `json:"targetKind,omitempty"`
	TargetName    string    `json:"targetName,omitempty"`
	Plan          PlanBody  `json:"plan"`
	Risk          string    `json:"risk"` // low|medium|high|denied
	Applied       bool      `json:"applied"`
	CreatedAt     time.Time `json:"createdAt"`
}

// PlanBody mirrors a minimal PlanResult.plan payload.
type PlanBody struct {
	Summary string   `json:"summary"`
	Steps   []string `json:"steps"`
}

// AuditEntry is appended to the local audit log.
type AuditEntry struct {
	At       time.Time `json:"at"`
	Proposal Proposal  `json:"proposal"`
}

// Engine evaluates policy and emits proposals (MVP: no cluster mutate in Propose).
type Engine struct {
	Policy Policy
	Audit  AuditStore

	mu sync.Mutex
}

// AuditStore persists proposals. FileAudit is the default local store.
type AuditStore interface {
	Append(entry AuditEntry) error
}

// FileAudit writes JSONL under Dir.
type FileAudit struct {
	Dir string
}

func (a FileAudit) path() string {
	return filepath.Join(a.Dir, "autopilot-audit.jsonl")
}

func (a FileAudit) Append(entry AuditEntry) error {
	if err := os.MkdirAll(a.Dir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(a.path(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

// MemAudit is an in-memory audit for tests.
type MemAudit struct {
	mu      sync.Mutex
	Entries []AuditEntry
}

func (a *MemAudit) Append(entry AuditEntry) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Entries = append(a.Entries, entry)
	return nil
}

// DefaultPolicy returns propose-only with rollback allowed.
func DefaultPolicy() Policy {
	return Policy{
		Allow:         []string{ActionRollbackFailedRollout},
		Apply:         false,
		MinConfidence: DefaultMinConfidence,
	}
}

// EvaluateAction returns denied if action is outside global MVP allowlist or policy Allow.
func EvaluateAction(policy Policy, actionID string) (decision string, reason string) {
	actionID = strings.TrimSpace(actionID)
	if !inList(MVPAllowlist, actionID) {
		return DecisionDenied, "hard-deny: action not in Autopilot MVP allowlist (ADR-0015)"
	}
	if !inList(policy.Allow, actionID) {
		return DecisionDenied, "hard-deny: action not in namespace policy allowlist"
	}
	return DecisionApproved, ""
}

// ProposeFromContext builds a proposal when context looks like a failed rollout.
// Never sets Applied=true; call ApplyGate separately (still no-op mutate in MVP binary path).
func (e *Engine) ProposeFromContext(agentCtx ctxbuild.AgentContext, confidence float64) (*Proposal, error) {
	if e == nil {
		return nil, fmt.Errorf("autopilot: engine is nil")
	}
	pol := e.Policy
	if pol.MinConfidence <= 0 {
		pol.MinConfidence = DefaultMinConfidence
	}
	action, targetKind, targetName, ok := detectAction(agentCtx)
	if !ok {
		return nil, nil
	}
	decision, reason := EvaluateAction(pol, action)
	if decision == DecisionDenied {
		p := baseProposal(agentCtx, action, targetKind, targetName, confidence)
		p.Decision = DecisionDenied
		p.Reason = reason
		p.Risk = "denied"
		p.Plan = PlanBody{Summary: "Denied Autopilot action", Steps: []string{reason}}
		_ = e.audit(p)
		return &p, nil
	}
	if confidence < pol.MinConfidence {
		p := baseProposal(agentCtx, action, targetKind, targetName, confidence)
		p.Decision = DecisionDenied
		p.Reason = fmt.Sprintf("confidence %.2f below floor %.2f", confidence, pol.MinConfidence)
		p.Risk = "denied"
		p.Plan = PlanBody{Summary: "Confidence gate failed", Steps: []string{p.Reason}}
		_ = e.audit(p)
		return &p, nil
	}

	p := baseProposal(agentCtx, action, targetKind, targetName, confidence)
	p.Decision = DecisionProposed
	p.Risk = "medium"
	p.Plan = planFor(action, agentCtx.Namespace, targetName)
	p.Reason = "proposeOnly (ADR-0015 MVP default); set policy.apply=true for policyAuto later"
	if pol.Apply {
		// MVP still does not mutate the cluster in-process — emit approved proposal for an external applier.
		p.Decision = DecisionApproved
		p.Reason = "policyAuto approved proposal; apply executor not enabled in this MVP binary (PlanResult gate preserved)"
		p.Applied = false
	}
	_ = e.audit(p)
	return &p, nil
}

func (e *Engine) audit(p Proposal) error {
	if e.Audit == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.Audit.Append(AuditEntry{At: time.Now().UTC(), Proposal: p})
}

func detectAction(agentCtx ctxbuild.AgentContext) (action, kind, name string, ok bool) {
	blob := strings.ToLower(agentCtx.Incident.Summary + " " + agentCtx.Incident.RootCause)
	for _, e := range agentCtx.Incident.Evidence {
		blob += " " + strings.ToLower(e.Reason+" "+e.Message)
	}
	name = ""
	kind = "Deployment"
	if agentCtx.Deployment != nil {
		name = agentCtx.Deployment.Name
	}
	if agentCtx.Target != nil && agentCtx.Target.Name != "" {
		if agentCtx.Target.Kind == "" || strings.EqualFold(agentCtx.Target.Kind, "Deployment") || strings.EqualFold(agentCtx.Target.Kind, "Pod") {
			if name == "" {
				name = agentCtx.Target.Name
			}
		}
		if agentCtx.Target.Kind != "" {
			kind = agentCtx.Target.Kind
		}
	}
	if name == "" && agentCtx.Incident.PrimaryResource != nil {
		name = agentCtx.Incident.PrimaryResource.Name
		if agentCtx.Incident.PrimaryResource.Kind != "" {
			kind = agentCtx.Incident.PrimaryResource.Kind
		}
	}
	failedRollout := strings.Contains(blob, "progressdeadline") ||
		strings.Contains(blob, "rollout") && (strings.Contains(blob, "failed") || strings.Contains(blob, "timed out") || strings.Contains(blob, "timeout")) ||
		(agentCtx.Deployment != nil && agentCtx.Deployment.ReadyReplicas < agentCtx.Deployment.DesiredReplicas && agentCtx.Deployment.DesiredReplicas > 0 &&
			(strings.Contains(blob, "failed") || strings.Contains(blob, "crashloop") || strings.Contains(blob, "imagepull")))
	if failedRollout && name != "" {
		return ActionRollbackFailedRollout, "Deployment", trimPodToDeploy(name), true
	}
	return "", "", "", false
}

func trimPodToDeploy(name string) string {
	// best-effort: api-7d9f-xk → api when looks like replica pod; else keep
	parts := strings.Split(name, "-")
	if len(parts) >= 3 {
		return strings.Join(parts[:len(parts)-2], "-")
	}
	return name
}

func planFor(action, ns, name string) PlanBody {
	switch action {
	case ActionRollbackFailedRollout:
		return PlanBody{
			Summary: fmt.Sprintf("Rollback Deployment/%s in %s after failed rollout", name, ns),
			Steps: []string{
				fmt.Sprintf("kubectl -n %s rollout undo deployment/%s", ns, name),
				fmt.Sprintf("kubectl -n %s rollout status deployment/%s", ns, name),
			},
		}
	default:
		return PlanBody{Summary: "unknown", Steps: nil}
	}
}

func baseProposal(agentCtx ctxbuild.AgentContext, action, kind, name string, confidence float64) Proposal {
	id := fmt.Sprintf("ap-%s-%d", action, time.Now().UTC().UnixNano())
	return Proposal{
		APIVersion:    APIVersion,
		Kind:          KindProposal,
		SchemaVersion: SchemaVersion,
		ID:            id,
		Namespace:     agentCtx.Namespace,
		ActionID:      action,
		Confidence:    confidence,
		IncidentID:    agentCtx.Incident.ID,
		TargetKind:    kind,
		TargetName:    name,
		Applied:       false,
		CreatedAt:     time.Now().UTC(),
	}
}

func inList(list []string, v string) bool {
	for _, x := range list {
		if strings.EqualFold(strings.TrimSpace(x), v) {
			return true
		}
	}
	return false
}

// DefaultAuditDir returns ~/.config/kprompt/autopilot.
func DefaultAuditDir() string {
	if d := strings.TrimSpace(os.Getenv("KPROMPT_AUTOPILOT_AUDIT_DIR")); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".", ".kprompt-autopilot")
	}
	return filepath.Join(home, ".config", "kprompt", "autopilot")
}

// IncidentConfidence picks a confidence hint from alert/context.
func IncidentConfidence(inc incident.Incident, fallback float64) float64 {
	if inc.Confidence > 0 {
		return inc.Confidence
	}
	return fallback
}
