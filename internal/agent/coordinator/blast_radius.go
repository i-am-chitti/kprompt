package coordinator

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// BlastRadiusHop is one cross-namespace handoff edge as a blast-radius hop (AG-066).
type BlastRadiusHop struct {
	From   string `json:"from"`
	To     string `json:"to,omitempty"`
	Count  int    `json:"count"`
	LastAt string `json:"lastAt,omitempty"`
	Risk   string `json:"risk"` // low | medium | high (heuristic from handoff count)
	Kind   string `json:"kind"` // cross-namespace-handoff
}

// BlastRadiusReport is the Coordinator blast-radius product graph MVP.
// Built from Shared Knowledge handoff edges — not a continuous mesh/OTel topology.
type BlastRadiusReport struct {
	APIVersion     string           `json:"apiVersion"`
	Kind           string           `json:"kind"`
	SchemaVersion  string           `json:"schemaVersion"`
	GeneratedAt    time.Time        `json:"generatedAt"`
	FocusNamespace string           `json:"focusNamespace,omitempty"`
	HandoffCount   int              `json:"handoffCount"`
	Namespaces     []string         `json:"namespaces,omitempty"`
	Hops           []BlastRadiusHop `json:"hops,omitempty"`
	Durable        bool             `json:"durable"`
	Note           string           `json:"note,omitempty"`
}

const kindBlastRadius = "CoordinatorBlastRadius"

// BlastRadius builds the MVP blast-radius view from the recent handoff ring.
// focusNamespace empty → all edges; otherwise hops touching that namespace.
func BlastRadius(records []Record, durable bool, focusNamespace string) BlastRadiusReport {
	sum := Summarize(records, durable)
	focus := strings.TrimSpace(focusNamespace)
	hops := make([]BlastRadiusHop, 0, len(sum.Edges))
	nsSet := map[string]struct{}{}

	for _, e := range sum.Edges {
		if focus != "" && e.From != focus && e.Suspect != focus {
			continue
		}
		if e.From != "" {
			nsSet[e.From] = struct{}{}
		}
		if e.Suspect != "" {
			nsSet[e.Suspect] = struct{}{}
		}
		hops = append(hops, BlastRadiusHop{
			From:   e.From,
			To:     e.Suspect,
			Count:  e.Count,
			LastAt: e.LastAt,
			Risk:   hopRisk(e.Count),
			Kind:   "cross-namespace-handoff",
		})
	}
	sort.Slice(hops, func(i, j int) bool {
		if hops[i].Count != hops[j].Count {
			return hops[i].Count > hops[j].Count
		}
		if hops[i].From != hops[j].From {
			return hops[i].From < hops[j].From
		}
		return hops[i].To < hops[j].To
	})

	ns := make([]string, 0, len(nsSet))
	for n := range nsSet {
		ns = append(ns, n)
	}
	sort.Strings(ns)

	note := "Coordinator blast-radius MVP: hops from Shared Knowledge handoffs only — not a continuous mesh/OTel product graph"
	if durable {
		note = "Coordinator blast-radius MVP: durable handoff hops (file/ConfigMap) — not a continuous mesh/OTel product graph"
	}

	return BlastRadiusReport{
		APIVersion:     APIVersion,
		Kind:           kindBlastRadius,
		SchemaVersion:  SchemaVersion,
		GeneratedAt:    time.Now().UTC(),
		FocusNamespace: focus,
		HandoffCount:   sum.HandoffCount,
		Namespaces:     ns,
		Hops:           hops,
		Durable:        durable,
		Note:           note,
	}
}

func hopRisk(count int) string {
	switch {
	case count >= 5:
		return "high"
	case count >= 2:
		return "medium"
	default:
		return "low"
	}
}

// FormatBlastRadius renders a compact human-readable blast-radius view.
func FormatBlastRadius(r BlastRadiusReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Coordinator blast-radius (MVP) · durable=%v\n", r.Durable)
	if r.FocusNamespace != "" {
		fmt.Fprintf(&b, "Focus: %s\n", r.FocusNamespace)
	}
	fmt.Fprintf(&b, "Handoffs remembered: %d · hops: %d\n", r.HandoffCount, len(r.Hops))
	if len(r.Namespaces) > 0 {
		fmt.Fprintf(&b, "Namespaces: %s\n", strings.Join(r.Namespaces, ", "))
	}
	for _, h := range r.Hops {
		if h.To == "" {
			fmt.Fprintf(&b, "- %s (no suspect) x%d risk=%s\n", h.From, h.Count, h.Risk)
			continue
		}
		fmt.Fprintf(&b, "- %s -> %s x%d risk=%s\n", h.From, h.To, h.Count, h.Risk)
	}
	if r.Note != "" {
		fmt.Fprintf(&b, "%s\n", r.Note)
	}
	return strings.TrimSpace(b.String())
}

// BlastRadius returns the MVP view for the current ring.
func (s *Service) BlastRadius(focusNamespace string) BlastRadiusReport {
	return BlastRadius(s.Recent(), s.Durable(), focusNamespace)
}

func (h *Handler) blastRadius(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	focus := strings.TrimSpace(r.URL.Query().Get("namespace"))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(h.Service.BlastRadius(focus))
}
