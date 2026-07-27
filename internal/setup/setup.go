// Package setup builds dry-run bootstrap plans from tools.Detect (T-062 · ADR-0018).
//
// Detect + plan only in this task — host/cluster apply lands in T-063 / T-064.
// Never mutates the cluster or host silently.
package setup

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/kprompt/kprompt/internal/tools"
)

// Profiles (T-065 will deepen flags; T-062 uses these to select plan components).
const (
	ProfileMinimal  = "minimal"  // Helm CLI
	ProfilePlatform = "platform" // Helm + Argo Workflows + Prometheus
	ProfileFull     = "full"     // platform + Grafana + OTel URL config
)

// Lanes match ADR-0018.
const (
	LaneHost    = "host"
	LaneCluster = "cluster"
	LaneConfig  = "config"
)

// Step status values.
const (
	StatusReady   = "ready"   // already available
	StatusNeeded  = "needed"  // gap; would install/configure on approve (later tasks)
	StatusBlocked = "blocked" // cannot propose (e.g. no kube for cluster lane)
	StatusSkipped = "skipped" // outside selected profile
)

// Options configures plan generation.
type Options struct {
	Profile string
	DryRun  bool // always true for T-062 apply path; kept for JSON honesty
}

// Step is one proposed bootstrap action (or ready confirmation).
type Step struct {
	ID         string   `json:"id"`
	Component  string   `json:"component"`
	Lane       string   `json:"lane"` // host | cluster | config
	Status     string   `json:"status"`
	Title      string   `json:"title"`
	Detail     string   `json:"detail,omitempty"`
	Risk       string   `json:"risk,omitempty"` // none | low | medium | high
	ActionHint string   `json:"actionHint,omitempty"`
	Commands   []string `json:"commands,omitempty"` // illustrative — not executed in T-062
}

// Plan is the stable human + JSON contract for `kprompt setup`.
type Plan struct {
	Type       string   `json:"type"`
	Profile    string   `json:"profile"`
	DryRun     bool     `json:"dryRun"`
	Summary    string   `json:"summary"`
	Needed     int      `json:"needed"`
	Ready      int      `json:"ready"`
	Steps      []Step   `json:"steps"`
	Notes      []string `json:"notes,omitempty"`
	DetectHint string   `json:"detectHint,omitempty"`
}

// NormalizeProfile returns a known profile or an error.
func NormalizeProfile(p string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "", ProfilePlatform:
		return ProfilePlatform, nil
	case ProfileMinimal:
		return ProfileMinimal, nil
	case ProfileFull:
		return ProfileFull, nil
	default:
		return "", fmt.Errorf("unknown setup profile %q (want minimal|platform|full)", p)
	}
}

// BuildPlan turns a detect registry into a dry-run setup plan.
func BuildPlan(reg *tools.Registry, opts Options) (Plan, error) {
	profile, err := NormalizeProfile(opts.Profile)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{
		Type:    "setup",
		Profile: profile,
		DryRun:  true, // T-062 never applies
		Steps:   make([]Step, 0, 8),
		Notes: []string{
			"Dry-run only — no host or cluster mutations (ADR-0018 · T-062).",
			"Host install apply: T-063 · Cluster operator apply: T-064 · Profiles/flags: T-065.",
			"Re-check with: kprompt tools · kprompt doctor",
		},
	}

	want := componentsForProfile(profile)
	kube, _ := reg.Get(tools.IDKubernetes)
	kubeOK := kube.Available()

	for _, c := range want {
		step := buildStep(reg, c, kubeOK)
		plan.Steps = append(plan.Steps, step)
		switch step.Status {
		case StatusNeeded:
			plan.Needed++
		case StatusReady:
			plan.Ready++
		}
	}

	switch {
	case plan.Needed == 0:
		plan.Summary = fmt.Sprintf(
			"Profile %q: all %d components ready — nothing to install or configure.",
			profile, plan.Ready,
		)
	default:
		plan.Summary = fmt.Sprintf(
			"Profile %q: %d needed, %d ready (dry-run plan — approve/apply not run).",
			profile, plan.Needed, plan.Ready,
		)
	}
	if !kubeOK {
		plan.Notes = append(plan.Notes,
			"Kubernetes unreachable — cluster-lane proposals are blocked until kubeconfig works.",
		)
	}
	plan.DetectHint = "Source: tools.Detect (same inventory as kprompt tools)"
	return plan, nil
}

type componentSpec struct {
	ID        tools.ID
	Name      string
	Lane      string
	Risk      string
	Action    string
	Commands  func(r tools.Result) []string
	ConfigMsg string
}

func componentsForProfile(profile string) []componentSpec {
	helm := componentSpec{
		ID: tools.IDHelm, Name: "Helm", Lane: LaneHost, Risk: "low",
		Action: "install-host",
		Commands: func(tools.Result) []string {
			return []string{
				"# macOS (Homebrew)",
				"brew install helm",
				"# or: https://helm.sh/docs/intro/install/",
			}
		},
	}
	argo := componentSpec{
		ID: tools.IDArgoWorkflows, Name: "Argo Workflows", Lane: LaneCluster, Risk: "medium",
		Action: "install-cluster",
		Commands: func(tools.Result) []string {
			return []string{
				"# illustrative — T-064 will wrap plan→approve",
				"kubectl create namespace argo",
				"kubectl apply -n argo -f https://github.com/argoproj/argo-workflows/releases/latest/download/install.yaml",
				"# docs: https://argo-workflows.readthedocs.io/en/latest/quick-start/",
			}
		},
	}
	prom := componentSpec{
		ID: tools.IDPrometheus, Name: "Prometheus", Lane: LaneConfig, Risk: "none",
		Action: "configure-url",
		Commands: func(tools.Result) []string {
			return []string{
				"kprompt config set tools.prometheus.url http://prometheus.monitoring.svc:9090",
				"# or: export KPROMPT_PROMETHEUS_URL=https://…",
				"# optional later: install kube-prometheus-stack via T-064",
			}
		},
		ConfigMsg: "Set Prometheus URL in config/env (prefer configure over install when a stack already exists).",
	}
	grafana := componentSpec{
		ID: tools.IDGrafana, Name: "Grafana", Lane: LaneConfig, Risk: "none",
		Action: "configure-url",
		Commands: func(tools.Result) []string {
			return []string{
				"kprompt config set tools.grafana.url http://grafana.monitoring.svc:3000",
				"# or: export KPROMPT_GRAFANA_URL=https://…",
			}
		},
	}
	otel := componentSpec{
		ID: tools.IDOpenTelemetry, Name: "OpenTelemetry", Lane: LaneConfig, Risk: "none",
		Action: "configure-url",
		Commands: func(tools.Result) []string {
			return []string{
				"kprompt config set tools.otel.endpoint http://jaeger-query.observability.svc:16686",
				"kprompt config set tools.otel.backend jaeger",
				"# or Tempo: tools.otel.backend=tempo",
			}
		},
	}

	switch profile {
	case ProfileMinimal:
		return []componentSpec{helm}
	case ProfileFull:
		return []componentSpec{helm, argo, prom, grafana, otel}
	default: // platform
		return []componentSpec{helm, argo, prom}
	}
}

func buildStep(reg *tools.Registry, c componentSpec, kubeOK bool) Step {
	r, ok := reg.Get(c.ID)
	step := Step{
		ID:        string(c.ID),
		Component: string(c.ID),
		Lane:      c.Lane,
		Title:     c.Name,
	}
	if !ok {
		step.Status = StatusBlocked
		step.Detail = "not present in detect registry"
		step.Risk = c.Risk
		return step
	}
	step.Detail = r.Detail

	if r.Status == tools.StatusDisabled {
		step.Status = StatusSkipped
		step.Detail = r.Detail
		step.ActionHint = "none"
		step.Risk = "none"
		return step
	}

	if r.Available() {
		step.Status = StatusReady
		step.ActionHint = "none"
		step.Risk = "none"
		step.Title = c.Name + " ready"
		return step
	}

	// Unavailable / gap
	if c.Lane == LaneCluster && !kubeOK {
		step.Status = StatusBlocked
		step.Title = c.Name + " (blocked)"
		step.Detail = "Kubernetes unreachable — cannot propose cluster install"
		step.Risk = c.Risk
		step.ActionHint = "fix-kube"
		return step
	}

	step.Status = StatusNeeded
	step.Title = c.Name + " needed"
	step.Risk = c.Risk
	step.ActionHint = c.Action
	if c.ConfigMsg != "" && step.Detail == "" {
		step.Detail = c.ConfigMsg
	}
	if r.Hint != "" {
		if step.Detail != "" {
			step.Detail = step.Detail + " — " + r.Hint
		} else {
			step.Detail = r.Hint
		}
	}
	if c.Commands != nil {
		step.Commands = c.Commands(r)
	}
	return step
}

// FormatText prints a human-readable dry-run plan.
func FormatText(w io.Writer, plan Plan) error {
	fmt.Fprintf(w, "Setup plan (profile=%s, dry-run=%v)\n", plan.Profile, plan.DryRun)
	fmt.Fprintf(w, "Summary: %s\n\n", plan.Summary)

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "STATUS\tLANE\tCOMPONENT\tDETAIL")
	for _, s := range plan.Steps {
		detail := sanitizeTab(s.Detail)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", s.Status, s.Lane, s.Component, detail)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	for _, s := range plan.Steps {
		if s.Status != StatusNeeded || len(s.Commands) == 0 {
			continue
		}
		fmt.Fprintf(w, "\nProposed (%s · %s · risk=%s):\n", s.Component, s.Lane, s.Risk)
		for _, c := range s.Commands {
			fmt.Fprintf(w, "  %s\n", c)
		}
	}

	if len(plan.Notes) > 0 {
		fmt.Fprintln(w, "\nNotes:")
		for _, n := range plan.Notes {
			fmt.Fprintf(w, "  - %s\n", n)
		}
	}
	if plan.Needed > 0 {
		fmt.Fprintln(w, "\nNo mutations performed. When apply ships: review this plan, then use --approve (T-063/T-064).")
	}
	return nil
}

func sanitizeTab(s string) string {
	return strings.ReplaceAll(s, "\t", " ")
}
