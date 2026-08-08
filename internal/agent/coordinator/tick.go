package coordinator

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kprompt/kprompt/internal/agent/handoff"
	"github.com/kprompt/kprompt/internal/incident"
)

const (
	// DefaultMaxHops caps blast-radius BFS depth from focus (RT-011).
	DefaultMaxHops = 3
	// DefaultTickBudget is max edges re-probed per proactive tick (RT-009).
	DefaultTickBudget   = 5
	reasonProactiveTick = "proactive-tick"
	maxAuditKeep        = 100
)

// TickConfig configures the opt-in continuous correlation tick (RT-009).
type TickConfig struct {
	Interval time.Duration // 0 → disabled
	Budget   int           // max edges to re-probe per tick (default DefaultTickBudget)
	MaxHops  int           // cascade hop cap for blast-radius (RT-011)
}

// TickResult summarizes one proactive tick.
type TickResult struct {
	EdgesConsidered int      `json:"edgesConsidered"`
	Probed          int      `json:"probed"`
	Merged          int      `json:"merged"`
	Skipped         int      `json:"skipped"`
	MutateAttempted bool     `json:"mutateAttempted"` // always false
	Details         []string `json:"details,omitempty"`
}

// AuditEntry records handoff or proactive merges (RT-011). Never implies mutate.
type AuditEntry struct {
	At              time.Time `json:"at"`
	Kind            string    `json:"kind"` // handoff | proactive-tick
	From            string    `json:"from,omitempty"`
	Suspect         string    `json:"suspect,omitempty"`
	MutateAttempted bool      `json:"mutateAttempted"`
	Detail          string    `json:"detail,omitempty"`
}

// Tick re-scans Shared Knowledge edges and optionally re-probes suspects (RT-009).
// Does not wait for a new handoff POST. MutateAttempted stays false.
func (s *Service) Tick(ctx context.Context, cfg TickConfig) TickResult {
	out := TickResult{MutateAttempted: false}
	if s == nil {
		return out
	}
	budget := cfg.Budget
	if budget <= 0 {
		budget = DefaultTickBudget
	}
	sum := s.Knowledge()
	edges := append([]KnowledgeEdge(nil), sum.Edges...)
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Count != edges[j].Count {
			return edges[i].Count > edges[j].Count
		}
		return edges[i].LastAt > edges[j].LastAt
	})
	out.EdgesConsidered = len(edges)
	probed := 0
	for _, e := range edges {
		if probed >= budget {
			out.Skipped++
			continue
		}
		from := strings.TrimSpace(e.From)
		suspect := strings.TrimSpace(e.Suspect)
		if from == "" || suspect == "" {
			out.Skipped++
			continue
		}
		origin := s.latestOriginReport(from, suspect)
		env := handoff.Envelope{
			APIVersion:       handoff.APIVersion,
			Kind:             handoff.Kind,
			SchemaVersion:    handoff.SchemaVersion,
			FromNamespace:    from,
			SuspectNamespace: suspect,
			Reason:           reasonProactiveTick,
			CreatedAt:        time.Now().UTC(),
			Report:           origin,
		}
		probed++
		out.Probed++
		_, err := s.Handle(ctx, env)
		if err != nil {
			out.Details = append(out.Details, fmt.Sprintf("%s→%s: %v", from, suspect, err))
			continue
		}
		out.Merged++
		out.Details = append(out.Details, fmt.Sprintf("%s→%s merged mutate=false", from, suspect))
	}
	return out
}

func (s *Service) latestOriginReport(from, suspect string) incident.InvestigationReport {
	recs := s.Recent()
	for i := len(recs) - 1; i >= 0; i-- {
		rec := recs[i]
		if rec.Envelope.FromNamespace == from && rec.Envelope.SuspectNamespace == suspect {
			rep := rec.Envelope.Report
			if strings.TrimSpace(rep.Summary) == "" {
				rep.Summary = firstNonEmpty(rec.Reply.Merged.Summary, "proactive tick refresh")
			}
			if rep.Namespace == "" {
				rep.Namespace = from
			}
			if rep.APIVersion == "" {
				rep.APIVersion = incident.APIVersion
			}
			if rep.Kind == "" {
				rep.Kind = incident.KindInvestigationReport
			}
			if rep.SchemaVersion == "" {
				rep.SchemaVersion = incident.SchemaVersion2
			}
			return rep
		}
	}
	return incident.InvestigationReport{
		APIVersion:    incident.APIVersion,
		Kind:          incident.KindInvestigationReport,
		SchemaVersion: incident.SchemaVersion2,
		Namespace:     from,
		Summary:       fmt.Sprintf("proactive tick refresh %s→%s", from, suspect),
		CreatedAt:     time.Now().UTC(),
	}
}

func (s *Service) appendAudit(e AuditEntry) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audit = append(s.audit, e)
	if len(s.audit) > maxAuditKeep {
		s.audit = s.audit[len(s.audit)-maxAuditKeep:]
	}
}

// Audit returns a copy of recent Coordinator audit entries (RT-011).
func (s *Service) Audit() []AuditEntry {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]AuditEntry, len(s.audit))
	copy(out, s.audit)
	return out
}

// RunTicker starts the proactive correlation loop until ctx is cancelled (RT-009).
// No-op when cfg.Interval <= 0.
func RunTicker(ctx context.Context, s *Service, cfg TickConfig, logf func(string, ...any)) {
	if s == nil || cfg.Interval <= 0 {
		return
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	t := time.NewTicker(cfg.Interval)
	defer t.Stop()
	logf("coordinator proactive tick interval=%s budget=%d (mutate=off)", cfg.Interval, max(cfg.Budget, DefaultTickBudget))
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			res := s.Tick(ctx, cfg)
			logf("proactive tick edges=%d probed=%d merged=%d skipped=%d mutate=%v",
				res.EdgesConsidered, res.Probed, res.Merged, res.Skipped, res.MutateAttempted)
		}
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
