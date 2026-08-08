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
	Kind   string `json:"kind"` // cross-namespace-handoff | mesh-otel
	Hop    int    `json:"hop,omitempty"` // distance from focus (RT-011)
}

// BlastRadiusReport is the Coordinator blast-radius product graph MVP.
// Built from Shared Knowledge handoff edges — mesh/OTel enrichment is opt-in (RT-010).
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
	MaxHops        int              `json:"maxHops,omitempty"`
	Status         string           `json:"status"` // ok | degraded (RT-010)
	Note           string           `json:"note,omitempty"`
}

const kindBlastRadius = "CoordinatorBlastRadius"

// BlastRadius builds the MVP blast-radius view from the recent handoff ring.
// focusNamespace empty → all edges; otherwise hops touching that namespace within maxHops.
func BlastRadius(records []Record, durable bool, focusNamespace string, maxHops int, meshConfigured bool) BlastRadiusReport {
	if maxHops <= 0 {
		maxHops = DefaultMaxHops
	}
	sum := Summarize(records, durable)
	focus := strings.TrimSpace(focusNamespace)
	hops := make([]BlastRadiusHop, 0, len(sum.Edges))
	nsSet := map[string]struct{}{}

	for _, e := range sum.Edges {
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

	if focus != "" {
		hops = filterHopsByDistance(hops, focus, maxHops)
		nsSet = map[string]struct{}{}
		for _, h := range hops {
			if h.From != "" {
				nsSet[h.From] = struct{}{}
			}
			if h.To != "" {
				nsSet[h.To] = struct{}{}
			}
		}
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

	status := "degraded"
	note := "Coordinator blast-radius: handoff hops only — mesh/OTel not configured (status=degraded, RT-010)"
	if meshConfigured {
		status = "ok"
		note = "Coordinator blast-radius: handoff hops + mesh/OTel enrichment available (RT-010)"
	}
	if durable {
		note += "; durable Shared Knowledge"
	}
	note += fmt.Sprintf("; maxHops=%d (RT-011); continuous tick ≠ silent heal", maxHops)

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
		MaxHops:        maxHops,
		Status:         status,
		Note:           note,
	}
}

// filterHopsByDistance keeps edges within maxHops BFS distance of focus (RT-011).
func filterHopsByDistance(hops []BlastRadiusHop, focus string, maxHops int) []BlastRadiusHop {
	if focus == "" || maxHops <= 0 {
		return hops
	}
	adj := map[string][]string{}
	for _, h := range hops {
		a, b := h.From, h.To
		if a == "" || b == "" {
			continue
		}
		adj[a] = append(adj[a], b)
		adj[b] = append(adj[b], a)
	}
	dist := map[string]int{focus: 0}
	queue := []string{focus}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if dist[cur] >= maxHops {
			continue
		}
		for _, next := range adj[cur] {
			if _, seen := dist[next]; seen {
				continue
			}
			dist[next] = dist[cur] + 1
			queue = append(queue, next)
		}
	}
	out := make([]BlastRadiusHop, 0, len(hops))
	seen := map[string]struct{}{}
	for _, h := range hops {
		df, okF := dist[h.From]
		if h.To == "" {
			if okF && df <= maxHops {
				cp := h
				cp.Hop = df
				out = append(out, cp)
			}
			continue
		}
		dt, okT := dist[h.To]
		if !okF || !okT {
			continue
		}
		hop := df
		if dt > hop {
			hop = dt
		}
		if hop > maxHops {
			continue
		}
		key := h.From + "|" + h.To
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		cp := h
		cp.Hop = hop
		out = append(out, cp)
	}
	return out
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
	fmt.Fprintf(&b, "Coordinator blast-radius · status=%s · durable=%v · maxHops=%d\n", r.Status, r.Durable, r.MaxHops)
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
		fmt.Fprintf(&b, "- %s -> %s x%d risk=%s hop=%d\n", h.From, h.To, h.Count, h.Risk, h.Hop)
	}
	if r.Note != "" {
		fmt.Fprintf(&b, "%s\n", r.Note)
	}
	return strings.TrimSpace(b.String())
}

// BlastRadius returns the MVP view for the current ring.
func (s *Service) BlastRadius(focusNamespace string) BlastRadiusReport {
	maxHops := DefaultMaxHops
	mesh := false
	if s != nil {
		if s.MaxHops > 0 {
			maxHops = s.MaxHops
		}
		mesh = s.MeshConfigured
	}
	return BlastRadius(s.Recent(), s.Durable(), focusNamespace, maxHops, mesh)
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
