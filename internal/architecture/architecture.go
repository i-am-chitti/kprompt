// Package architecture narrates a high-level cluster/platform shape (S-012).
//
// Combines learn tool profile + service graph + heuristic infra deps.
// Template narrative only — not an LLM essay. Honest when the profile is thin.
package architecture

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"k8s.io/client-go/kubernetes"

	"github.com/kprompt/kprompt/internal/agent/memory"
	"github.com/kprompt/kprompt/internal/config"
	"github.com/kprompt/kprompt/internal/graph"
	"github.com/kprompt/kprompt/internal/learn"
)

const (
	TypeArchitecture = "ArchitectureNarrative"

	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
)

// Request scopes an architecture narrative.
type Request struct {
	Namespace string // empty = cluster-wide graph
	Prompt    string
	Context   string // kube context for learn profile
	File      config.File
	// Profile overrides LoadBestEffort (tests).
	Profile *learn.Profile
	// Graph overrides graph.Build (tests).
	Graph *graph.Report
	// Deps overrides memory.Discover (tests).
	Deps []memory.Fact
}

// Component is one named building block in the narrative.
type Component struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"` // platform | ingress | gitops | mesh | observability | data | workload
	Source   string `json:"source"`   // learn | graph | discover
	Detail   string `json:"detail,omitempty"`
}

// Report is the typed architecture narrative payload.
type Report struct {
	Type           string      `json:"type"`
	Scope          string      `json:"scope"`
	Namespace      string      `json:"namespace,omitempty"`
	ClusterContext string      `json:"cluster_context,omitempty"`
	Narrative      string      `json:"narrative"`
	Confidence     string      `json:"confidence"` // high | medium | low
	Summary        string      `json:"summary"`
	Components     []Component `json:"components"`
	Hints          []string    `json:"hints,omitempty"`
	Degraded       []string    `json:"degraded,omitempty"`
}

// Analyzer builds the narrative from live signals.
type Analyzer struct {
	Client kubernetes.Interface
}

// Run collects learn + graph + discover signals and narrates them.
func (a *Analyzer) Run(ctx context.Context, req Request) (Report, error) {
	if a == nil || a.Client == nil {
		return Report{}, fmt.Errorf("architecture: client required")
	}
	ns := strings.TrimSpace(req.Namespace)
	var degraded []string

	prof := learn.Profile{}
	if req.Profile != nil {
		prof = *req.Profile
	} else {
		p, ok := learn.LoadBestEffort(req.Context)
		if ok {
			prof = p
		} else {
			degraded = append(degraded, "learn profile missing — run: kprompt learn")
		}
	}

	var gRep graph.Report
	if req.Graph != nil {
		gRep = *req.Graph
	} else {
		built, err := graph.Build(ctx, a.Client, graph.Request{
			Namespace:            ns,
			IncludeNetworkPolicy: true,
			IncludeIngress:       true,
			IncludePVC:           true,
			IncludeVolumeRefs:    true,
		})
		if err != nil {
			return Report{}, fmt.Errorf("architecture graph: %w", err)
		}
		gRep = built
		degraded = append(degraded, gRep.Notes...)
	}

	var deps []memory.Fact
	if req.Deps != nil {
		deps = req.Deps
	} else {
		facts, err := memory.Discover(ctx, a.Client, ns)
		if err != nil {
			degraded = append(degraded, "dependency discover: "+err.Error())
		} else {
			deps = facts
		}
	}

	rep := FromSignals(prof, gRep, deps, ns)
	rep.ClusterContext = strings.TrimSpace(req.Context)
	rep.Degraded = appendUnique(rep.Degraded, degraded...)
	return rep, nil
}

// FromSignals is the pure narrative compiler (testable).
func FromSignals(prof learn.Profile, gRep graph.Report, deps []memory.Fact, ns string) Report {
	scope := graph.ScopeCluster
	ns = strings.TrimSpace(ns)
	if ns != "" {
		scope = graph.ScopeNamespace
	}

	comps := collectComponents(prof, gRep, deps)
	confidence := confidenceFor(prof, gRep, comps)
	narrative := buildNarrative(ns, comps, gRep, confidence)
	hints := buildHints(prof, confidence)

	scopeLabel := "cluster"
	if scope == graph.ScopeNamespace {
		scopeLabel = "namespace " + ns
	}
	summary := fmt.Sprintf(
		"Architecture narrative for %s (%s confidence, %d component(s))",
		scopeLabel, confidence, len(comps),
	)

	return Report{
		Type:       TypeArchitecture,
		Scope:      scope,
		Namespace:  ns,
		Narrative:  narrative,
		Confidence: confidence,
		Summary:    summary,
		Components: comps,
		Hints:      hints,
	}
}

func collectComponents(prof learn.Profile, gRep graph.Report, deps []memory.Fact) []Component {
	seen := map[string]struct{}{}
	var out []Component
	add := func(c Component) {
		id := c.ID
		if id == "" {
			id = c.Category + "/" + c.Name
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		c.ID = id
		out = append(out, c)
	}

	for _, t := range prof.Tools {
		if !t.Available || t.ID == "kubernetes" {
			continue
		}
		add(Component{
			ID:       "learn/" + t.ID,
			Name:     t.Name,
			Category: categoryForTool(t.ID),
			Source:   "learn",
			Detail:   t.Detail,
		})
	}

	svcN, ingressN, pvcN := 0, 0, 0
	for _, n := range gRep.Nodes {
		switch n.Kind {
		case graph.NodeService:
			svcN++
		case graph.NodeIngress:
			ingressN++
		case graph.NodePVC:
			pvcN++
		}
	}
	if svcN > 0 {
		add(Component{
			ID: "graph/services", Name: "Services", Category: "workload", Source: "graph",
			Detail: fmt.Sprintf("%d Service node(s) in graph", svcN),
		})
	}
	if ingressN > 0 {
		add(Component{
			ID: "graph/ingress", Name: "Ingress", Category: "ingress", Source: "graph",
			Detail: fmt.Sprintf("%d Ingress object(s)", ingressN),
		})
	}
	if pvcN > 0 {
		add(Component{
			ID: "graph/pvc", Name: "PersistentVolumeClaims", Category: "data", Source: "graph",
			Detail: fmt.Sprintf("%d PVC mount edge(s)/node(s)", pvcN),
		})
	}

	for _, f := range deps {
		if f.Kind != memory.KindDependency {
			continue
		}
		add(Component{
			ID:       "discover/" + f.Key,
			Name:     displayDep(f.Key),
			Category: "data",
			Source:   "discover",
			Detail:   strings.TrimSpace(f.Evidence + " (" + f.Value + ")"),
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return catRank(out[i].Category) < catRank(out[j].Category)
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func buildNarrative(ns string, comps []Component, gRep graph.Report, confidence string) string {
	scope := "this cluster"
	if ns != "" {
		scope = "namespace " + ns
	}

	var platform, ingress, gitops, mesh, obs, data []string
	for _, c := range comps {
		switch c.Category {
		case "platform":
			platform = append(platform, c.Name)
		case "ingress":
			ingress = append(ingress, c.Name)
		case "gitops":
			gitops = append(gitops, c.Name)
		case "mesh":
			mesh = append(mesh, c.Name)
		case "observability":
			obs = append(obs, c.Name)
		case "data":
			if c.ID != "graph/pvc" && c.ID != "graph/services" {
				data = append(data, c.Name)
			}
		}
	}

	var parts []string
	if confidence == ConfidenceLow {
		parts = append(parts, fmt.Sprintf(
			"%s has a thin signal set — treat this as a sketch, not a proven platform map.",
			capitalize(scope),
		))
	}

	headline := fmt.Sprintf("%s looks like", capitalize(scope))
	var shape []string
	if len(ingress) > 0 {
		shape = append(shape, joinShort(ingress))
	}
	if len(gitops) > 0 {
		shape = append(shape, joinShort(gitops))
	}
	if len(mesh) > 0 {
		shape = append(shape, joinShort(mesh))
	}
	if len(data) > 0 {
		shape = append(shape, joinShort(data))
	}
	if len(obs) > 0 {
		shape = append(shape, joinShort(obs))
	}
	if len(platform) > 0 {
		shape = append(shape, joinShort(platform))
	}

	if len(shape) == 0 {
		svcCount := 0
		for _, n := range gRep.Nodes {
			if n.Kind == graph.NodeService {
				svcCount++
			}
		}
		if svcCount > 0 {
			parts = append(parts, fmt.Sprintf(
				"%s a basic Kubernetes service layout (%d Service(s) in the graph) without a strong platform/tool fingerprint yet.",
				headline, svcCount,
			))
		} else {
			parts = append(parts, fmt.Sprintf(
				"%s an empty or inaccessible inventory — no Services or learned tools to describe.",
				headline,
			))
		}
	} else {
		parts = append(parts, fmt.Sprintf("%s: %s.", headline, strings.Join(shape, " + ")))
	}

	if edgeLine := summarizeEdges(gRep); edgeLine != "" {
		parts = append(parts, edgeLine)
	}
	return strings.Join(parts, " ")
}

func summarizeEdges(gRep graph.Report) string {
	exposes, mounts, selects := 0, 0, 0
	for _, e := range gRep.Edges {
		switch e.Type {
		case graph.EdgeExposes:
			exposes++
		case graph.EdgeMounts:
			mounts++
		case graph.EdgeSelects, graph.EdgeRoutes:
			selects++
		}
	}
	var bits []string
	if exposes > 0 {
		bits = append(bits, fmt.Sprintf("%d Ingress→Service expose edge(s)", exposes))
	}
	if selects > 0 {
		bits = append(bits, fmt.Sprintf("%d Service→Pod routing edge(s)", selects))
	}
	if mounts > 0 {
		bits = append(bits, fmt.Sprintf("%d volume mount edge(s)", mounts))
	}
	if len(bits) == 0 {
		return ""
	}
	return "Graph shows " + strings.Join(bits, ", ") + "."
}

func buildHints(prof learn.Profile, confidence string) []string {
	var hints []string
	if confidence == ConfidenceLow {
		hints = append(hints, "Run kprompt learn to detect Gateway API, GitOps, mesh, and observability tools.")
		hints = append(hints, "Narrow with -n <namespace> when explaining one app platform.")
	}
	if len(prof.Available) == 0 && confidence != ConfidenceLow {
		hints = append(hints, "No optional tools in the learn profile — narrative leans on the service graph only.")
	}
	hints = append(hints, "Heuristic deps (redis/kafka/…) are name/env hints, not proof of a managed data plane.")
	return hints
}

func confidenceFor(prof learn.Profile, gRep graph.Report, comps []Component) string {
	toolN := 0
	for _, t := range prof.Tools {
		if t.Available && t.ID != "kubernetes" {
			toolN++
		}
	}
	svcN := 0
	for _, n := range gRep.Nodes {
		if n.Kind == graph.NodeService {
			svcN++
		}
	}
	dataN := 0
	for _, c := range comps {
		if c.Source == "discover" {
			dataN++
		}
	}
	switch {
	case toolN >= 2 && (svcN >= 1 || dataN >= 1):
		return ConfidenceHigh
	case toolN >= 1 || (svcN >= 2 && dataN >= 1) || svcN >= 3:
		return ConfidenceMedium
	default:
		return ConfidenceLow
	}
}

func categoryForTool(id string) string {
	switch id {
	case "gateway-api":
		return "ingress"
	case "gitops":
		return "gitops"
	case "istio", "linkerd":
		return "mesh"
	case "prometheus", "grafana", "opentelemetry":
		return "observability"
	case "helm", "cert-manager", "crossplane", "keda", "tekton", "argo-workflows":
		return "platform"
	default:
		return "platform"
	}
}

func catRank(c string) int {
	switch c {
	case "ingress":
		return 0
	case "gitops":
		return 1
	case "mesh":
		return 2
	case "data":
		return 3
	case "observability":
		return 4
	case "platform":
		return 5
	case "workload":
		return 6
	default:
		return 9
	}
}

func displayDep(key string) string {
	switch key {
	case "postgres":
		return "Postgres"
	case "redis":
		return "Redis"
	case "kafka":
		return "Kafka"
	case "mongo":
		return "MongoDB"
	case "mysql":
		return "MySQL"
	case "rabbitmq":
		return "RabbitMQ"
	case "elasticsearch":
		return "Elasticsearch"
	case "nats":
		return "NATS"
	case "memcached":
		return "Memcached"
	default:
		if key == "" {
			return key
		}
		return strings.ToUpper(key[:1]) + key[1:]
	}
}

func joinShort(names []string) string {
	if len(names) == 0 {
		return ""
	}
	if len(names) == 1 {
		return names[0]
	}
	if len(names) == 2 {
		return names[0] + " + " + names[1]
	}
	return names[0] + " + " + names[1] + " +"
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func appendUnique(in []string, extra ...string) []string {
	seen := map[string]struct{}{}
	for _, s := range in {
		seen[s] = struct{}{}
	}
	for _, s := range extra {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		in = append(in, s)
	}
	return in
}
