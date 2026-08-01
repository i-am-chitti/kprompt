package roast

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strings"

	"github.com/kprompt/kprompt/internal/agent/health"
)

const (
	TypeClusterRoast = "ClusterRoast"
	ScopeNamespace   = "namespace"
	ScopeCluster     = "cluster"
)

// Report is a witty, read-only health roast for one namespace or a fleet rollup.
type Report struct {
	Type       string            `json:"type"`
	Scope      string            `json:"scope"`
	Namespace  string            `json:"namespace,omitempty"`
	Headline   string            `json:"headline"`
	Verdict    string            `json:"verdict"` // thriving | ok | rough | on_fire
	Score      int               `json:"score"`
	Trend      string            `json:"trend,omitempty"`
	PodReady   string            `json:"podReady,omitempty"`
	Restarts   int               `json:"restarts,omitempty"`
	Incidents  int               `json:"openIncidents,omitempty"`
	Namespaces []NamespaceRoast  `json:"namespaces,omitempty"`
	Tips       []string          `json:"tips,omitempty"`
}

// NamespaceRoast is one line in a cluster-wide roast.
type NamespaceRoast struct {
	Namespace string `json:"namespace"`
	Score     int    `json:"score"`
	Trend     string `json:"trend,omitempty"`
	PodReady  string `json:"podReady,omitempty"`
	Restarts  int    `json:"restarts,omitempty"`
	Incidents int    `json:"openIncidents,omitempty"`
	Line      string `json:"line"`
}

// FromSnapshot builds a namespace roast from an Observe health snapshot.
func FromSnapshot(snap health.Snapshot) Report {
	ns := strings.TrimSpace(snap.Namespace)
	verdict := verdictFor(snap.Score)
	r := Report{
		Type:      TypeClusterRoast,
		Scope:     ScopeNamespace,
		Namespace: ns,
		Headline:  pickHeadline(ns, snap.Score, snap.Trend, snap.Restarts, snap.OpenIncidents),
		Verdict:   verdict,
		Score:     snap.Score,
		Trend:     snap.Trend,
		PodReady:  snap.PodReady,
		Restarts:  snap.Restarts,
		Incidents: snap.OpenIncidents,
		Tips:      tipsFor(ns, snap),
	}
	return r
}

// FromFleet builds a cluster-wide roast, worst namespaces first.
func FromFleet(snaps []health.Snapshot) Report {
	items := make([]NamespaceRoast, 0, len(snaps))
	minScore := 100
	sum := 0
	worstNS := ""
	for _, snap := range snaps {
		if snap.Score < minScore {
			minScore = snap.Score
			worstNS = snap.Namespace
		}
		sum += snap.Score
		items = append(items, NamespaceRoast{
			Namespace: snap.Namespace,
			Score:     snap.Score,
			Trend:     snap.Trend,
			PodReady:  snap.PodReady,
			Restarts:  snap.Restarts,
			Incidents: snap.OpenIncidents,
			Line:      pickHeadline(snap.Namespace, snap.Score, snap.Trend, snap.Restarts, snap.OpenIncidents),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Score != items[j].Score {
			return items[i].Score < items[j].Score
		}
		return items[i].Namespace < items[j].Namespace
	})
	avg := 100
	if len(snaps) > 0 {
		avg = sum / len(snaps)
	}
	verdict := verdictFor(minScore)
	headline := fleetHeadline(len(snaps), avg, minScore, worstNS)
	tips := []string{
		"Read-only roast — nothing was mutated",
		"Dig in: kprompt agent run -n <ns> --health --heuristic",
	}
	if worstNS != "" && minScore < 80 {
		tips = append([]string{fmt.Sprintf("Start with the dumpster fire: kprompt \"how's my namespace\" -n %s", worstNS)}, tips...)
	}
	return Report{
		Type:       TypeClusterRoast,
		Scope:      ScopeCluster,
		Headline:   headline,
		Verdict:    verdict,
		Score:      minScore,
		Namespaces: items,
		Tips:       tips,
	}
}

// Format renders a human TTY roast.
func Format(r Report) string {
	var s strings.Builder
	fmt.Fprintf(&s, "🔥 Cluster roast")
	if r.Scope == ScopeNamespace && r.Namespace != "" {
		fmt.Fprintf(&s, " · %s", r.Namespace)
	} else {
		fmt.Fprintf(&s, " · fleet")
	}
	fmt.Fprintln(&s)
	fmt.Fprintf(&s, "%s\n", r.Headline)
	if r.Scope == ScopeNamespace {
		fmt.Fprintf(&s, "Score: %d/100 (%s)", r.Score, r.Verdict)
		if r.Trend != "" {
			fmt.Fprintf(&s, " · trend %s", r.Trend)
		}
		if r.PodReady != "" {
			fmt.Fprintf(&s, " · pods %s", r.PodReady)
		}
		if r.Restarts > 0 {
			fmt.Fprintf(&s, " · restarts %d", r.Restarts)
		}
		if r.Incidents > 0 {
			fmt.Fprintf(&s, " · open incidents %d", r.Incidents)
		}
		fmt.Fprintln(&s)
	} else {
		fmt.Fprintf(&s, "Worst score: %d/100 (%s) across %d namespace(s)\n", r.Score, r.Verdict, len(r.Namespaces))
		limit := len(r.Namespaces)
		if limit > 12 {
			limit = 12
		}
		for i := 0; i < limit; i++ {
			ns := r.Namespaces[i]
			fmt.Fprintf(&s, "  • %s — %d/100 — %s\n", ns.Namespace, ns.Score, ns.Line)
		}
		if len(r.Namespaces) > limit {
			fmt.Fprintf(&s, "  … %d more namespaces\n", len(r.Namespaces)-limit)
		}
	}
	for _, tip := range r.Tips {
		fmt.Fprintf(&s, "💡 %s\n", tip)
	}
	return strings.TrimRight(s.String(), "\n")
}

func verdictFor(score int) string {
	switch {
	case score >= 90:
		return "thriving"
	case score >= 70:
		return "ok"
	case score >= 40:
		return "rough"
	default:
		return "on_fire"
	}
}

func tipsFor(ns string, snap health.Snapshot) []string {
	tips := []string{"Read-only roast — nothing was mutated"}
	if snap.Score < 90 {
		if ns != "" {
			tips = append(tips, fmt.Sprintf("Next: kprompt agent run -n %s --health --heuristic", ns))
		} else {
			tips = append(tips, "Next: kprompt agent run --health --heuristic")
		}
	} else {
		tips = append(tips, "Optional flex: kprompt \"optimize my cluster\"")
	}
	if snap.Restarts > 5 {
		tips = append(tips, "Restarts are spicy — try kprompt \"explain why <workload> is crashing\"")
	}
	return tips
}

func fleetHeadline(n, avg, min int, worst string) string {
	switch {
	case n == 0:
		return "No namespaces to roast. Either the cluster is empty or RBAC ghosted us"
	case min >= 90:
		return fmt.Sprintf("%d namespaces and somehow they are all fine. Suspicious. Average %d/100", n, avg)
	case min >= 70:
		return fmt.Sprintf("Fleet is mostly upright (avg %d). Weakest link: %s at %d/100", avg, worst, min)
	case min >= 40:
		return fmt.Sprintf("Cluster has opinions. Worst: %s at %d/100 — avg %d across %d namespaces", worst, min, avg, n)
	default:
		return fmt.Sprintf("Someone call SRE — %s is at %d/100. Fleet avg %d across %d namespaces", worst, min, avg, n)
	}
}

var headlinesThriving = []string{
	"Annoyingly healthy. The pods are thriving and it is rude",
	"Green across the board. Where is the drama we paid for?",
	"Namespace looks gym-ready. Hydrate and stop touching it",
	"Health check passed. Ego optional, vigilance still required",
}

var headlinesOK = []string{
	"Mostly fine — like a demo that has not met production yet",
	"Not broken, not boring — a solid B-minus with room for chaos",
	"Holding together. A few pods are side-eyeing the Deployment",
	"Functional. Charming is a stretch. Keep an eye on restarts",
}

var headlinesRough = []string{
	"Rough day in the namespace. The Ready probes are writing poetry",
	"Health is mid. Somewhere a CrashLoopBackOff is warming up",
	"Not a wipe, but also not a vibe. Dig before the pager does",
	"Pods are negotiating with reality. Reality is winning",
}

var headlinesOnFire = []string{
	"On fire — and not the Prometheus kind. Bring a extinguisher and a plan",
	"This namespace has main-character energy, and the plot is CrashLoop",
	"Health score filed a complaint. The cluster would like a word",
	"Dumpster-adjacent. Named fixes only — mass delete fantasies stay denied",
}

func pickHeadline(ns string, score int, trend string, restarts, incidents int) string {
	pool := headlinesOK
	switch {
	case score >= 90:
		pool = headlinesThriving
	case score >= 70:
		pool = headlinesOK
	case score >= 40:
		pool = headlinesRough
	default:
		pool = headlinesOnFire
	}
	key := fmt.Sprintf("%s|%d|%s|%d|%d", ns, score, trend, restarts, incidents)
	line := pool[hashIndex(key, len(pool))]
	if trend == "risk_increasing" {
		return line + " (and the trend is risk_increasing — cute)"
	}
	if trend == "improving" && score < 90 {
		return line + " — improving, somehow"
	}
	return line
}

func hashIndex(key string, n int) int {
	if n <= 0 {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return int(h.Sum32()) % n
}
