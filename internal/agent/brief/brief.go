// Package brief builds a read-only Namespace Agent intelligence summary (AG-065).
package brief

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kprompt/kprompt/internal/agent/correlate"
	"github.com/kprompt/kprompt/internal/agent/health"
	"github.com/kprompt/kprompt/internal/agent/memory"
	"github.com/kprompt/kprompt/internal/agent/patterns"
	"github.com/kprompt/kprompt/internal/incident"
	"k8s.io/client-go/kubernetes"
)

// Brief is the Namespace Agent intelligence MVP surface.
type Brief struct {
	APIVersion    string          `json:"apiVersion"`
	Kind          string          `json:"kind"`
	SchemaVersion string          `json:"schemaVersion"`
	Namespace     string          `json:"namespace"`
	At            time.Time       `json:"at"`
	Health        *health.Snapshot `json:"health,omitempty"`
	OpenIncidents int             `json:"openIncidents"`
	IncidentIDs   []string        `json:"incidentIds,omitempty"`
	Patterns      int             `json:"patterns"`
	TopPatterns   []string        `json:"topPatterns,omitempty"`
	MemoryDeps    int             `json:"memoryDeps"`
	MemoryFacts   int             `json:"memoryFacts"`
	DepKeys       []string        `json:"depKeys,omitempty"`
	Notes         []string        `json:"notes,omitempty"`
}

const (
	kindBrief     = "NamespaceAgentBrief"
	apiVersion    = "kprompt.io/v1"
	schemaVersion = "1"
)

// Inputs are optional stores; missing backends become Notes (honest degrade).
type Inputs struct {
	Client   kubernetes.Interface
	Incidents correlate.Store
	Patterns  patterns.Store
	Memory    memory.Store
}

// Build composes health + Incident Memory + patterns for one namespace.
func Build(ctx context.Context, namespace string, in Inputs) (Brief, error) {
	ns := strings.TrimSpace(namespace)
	if ns == "" {
		return Brief{}, fmt.Errorf("namespace is required")
	}
	b := Brief{
		APIVersion:    apiVersion,
		Kind:          kindBrief,
		SchemaVersion: schemaVersion,
		Namespace:     ns,
		At:            time.Now().UTC(),
	}

	var open []incident.Incident
	if in.Incidents != nil {
		snap, err := in.Incidents.Load(ns)
		if err != nil {
			b.Notes = append(b.Notes, fmt.Sprintf("incidents: %v", err))
		} else {
			for id, inc := range snap.Open {
				open = append(open, inc)
				b.IncidentIDs = append(b.IncidentIDs, id)
			}
			b.OpenIncidents = len(open)
		}
	} else {
		b.Notes = append(b.Notes, "incidents store not configured")
	}

	tracker := health.NewTracker(ns, in.Client)
	hs := tracker.Evaluate(ctx, open)
	b.Health = &hs
	if b.OpenIncidents == 0 && hs.OpenIncidents > 0 {
		b.OpenIncidents = hs.OpenIncidents
	}

	if in.Patterns != nil {
		snap, err := patterns.New(in.Patterns).List(ns)
		if err != nil {
			b.Notes = append(b.Notes, fmt.Sprintf("patterns: %v", err))
		} else {
			b.Patterns = len(snap.Patterns)
			for i, p := range snap.Patterns {
				if i >= 5 {
					break
				}
				label := p.Signature
				if label == "" {
					label = p.ID
				}
				if p.Count > 0 {
					label = fmt.Sprintf("%s ×%d", label, p.Count)
				}
				b.TopPatterns = append(b.TopPatterns, label)
			}
		}
	} else {
		b.Notes = append(b.Notes, "patterns store not configured")
	}

	if in.Memory != nil {
		snap, err := memory.New(in.Memory).List(ns)
		if err != nil {
			b.Notes = append(b.Notes, fmt.Sprintf("memory: %v", err))
		} else {
			b.MemoryFacts = len(snap.Facts)
			for _, f := range snap.Facts {
				if f.Kind == memory.KindDependency {
					b.MemoryDeps++
					b.DepKeys = append(b.DepKeys, f.Key)
				}
			}
		}
	} else {
		b.Notes = append(b.Notes, "memory store not configured")
	}

	b.Notes = append(b.Notes, "Namespace Agent intelligence brief — propose-first; never auto-mutates (AG-065)")
	return b, nil
}

// Format renders a compact human-readable brief.
func Format(b Brief) string {
	var s strings.Builder
	fmt.Fprintf(&s, "Namespace Agent brief · %s\n", b.Namespace)
	if b.Health != nil {
		fmt.Fprintf(&s, "Health: %d (%s)", b.Health.Score, b.Health.Trend)
		if b.Health.PodReady != "" {
			fmt.Fprintf(&s, " · pods %s", b.Health.PodReady)
		}
		if b.Health.Restarts > 0 {
			fmt.Fprintf(&s, " · restarts %d", b.Health.Restarts)
		}
		fmt.Fprintln(&s)
		if b.Health.Message != "" {
			fmt.Fprintf(&s, "  %s\n", b.Health.Message)
		}
	}
	fmt.Fprintf(&s, "Open incidents: %d\n", b.OpenIncidents)
	for _, id := range b.IncidentIDs {
		fmt.Fprintf(&s, "  - %s\n", id)
	}
	fmt.Fprintf(&s, "Patterns: %d\n", b.Patterns)
	for _, p := range b.TopPatterns {
		fmt.Fprintf(&s, "  - %s\n", p)
	}
	fmt.Fprintf(&s, "Memory: %d facts (%d deps)\n", b.MemoryFacts, b.MemoryDeps)
	for _, k := range b.DepKeys {
		fmt.Fprintf(&s, "  - dep %s\n", k)
	}
	for _, n := range b.Notes {
		if strings.Contains(n, "AG-065") {
			continue
		}
		fmt.Fprintf(&s, "note: %s\n", n)
	}
	fmt.Fprintf(&s, "Propose-first · AG-065")
	return strings.TrimSpace(s.String())
}
