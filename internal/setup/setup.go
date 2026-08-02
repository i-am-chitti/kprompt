// Package setup builds bootstrap plans from tools.Detect and applies
// approve-gated host/cluster installs (ADR-0018 · T-062…T-065).
//
// Never mutates silently. Cluster path is install-only (wipe-class denied).
package setup

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/kprompt/kprompt/internal/tools"
)

// Profiles (ADR-0018 · T-065).
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
	StatusNeeded  = "needed"  // gap; would install/configure on approve
	StatusBlocked = "blocked" // cannot propose (e.g. no kube for cluster lane)
	StatusSkipped = "skipped" // outside selected profile / --only
)

// Options configures plan generation (T-065).
type Options struct {
	Profile string
	// Only limits the plan to these component ids (helm, argo-workflows, …).
	// Empty = all components in the profile. Values outside the profile error.
	Only []string
	// DryRun is recorded on the plan JSON (true when printing only).
	DryRun bool
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
	Commands   []string `json:"commands,omitempty"` // illustrative when dry-run
}

// Plan is the stable human + JSON contract for `kprompt setup`.
type Plan struct {
	Type       string   `json:"type"`
	Profile    string   `json:"profile"`
	Only       []string `json:"only,omitempty"`
	DryRun     bool     `json:"dryRun"`
	Summary    string   `json:"summary"`
	Needed     int      `json:"needed"`
	Ready      int      `json:"ready"`
	Steps      []Step   `json:"steps"`
	Notes      []string `json:"notes,omitempty"`
	DetectHint string   `json:"detectHint,omitempty"`
}

// ProfileInfo describes one curated profile for help / docs (T-065).
type ProfileInfo struct {
	ID         string
	Summary    string
	Components []string // tool ids
}

// Profiles lists curated OSS setup profiles.
func Profiles() []ProfileInfo {
	return []ProfileInfo{
		{
			ID: ProfileMinimal, Summary: "Helm CLI only (host lane)",
			Components: []string{string(tools.IDHelm)},
		},
		{
			ID: ProfilePlatform, Summary: "Helm + Argo Workflows + Prometheus stack (default)",
			Components: []string{
				string(tools.IDHelm), string(tools.IDArgoWorkflows), string(tools.IDPrometheus),
			},
		},
		{
			ID: ProfileFull, Summary: "platform + Grafana/OTel URL config steps (config lane; no auto-write)",
			Components: []string{
				string(tools.IDHelm), string(tools.IDArgoWorkflows), string(tools.IDPrometheus),
				string(tools.IDGrafana), string(tools.IDOpenTelemetry),
			},
		},
	}
}

// ProfilesDoc is embedded in CLI Long help.
func ProfilesDoc() string {
	var b strings.Builder
	b.WriteString("Profiles:\n")
	for _, p := range Profiles() {
		fmt.Fprintf(&b, "  %-8s  %s\n", p.ID, p.Summary)
		fmt.Fprintf(&b, "            components: %s\n", strings.Join(p.Components, ", "))
	}
	return strings.TrimRight(b.String(), "\n")
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

// NormalizeOnly parses --only values into canonical tool ids.
func NormalizeOnly(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		for _, part := range strings.Split(item, ",") {
			id, err := normalizeComponentID(part)
			if err != nil {
				return nil, err
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out, nil
}

func normalizeComponentID(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return "", fmt.Errorf("empty --only component")
	case "helm":
		return string(tools.IDHelm), nil
	case "argo", "argo-workflows", "workflows":
		return string(tools.IDArgoWorkflows), nil
	case "prometheus", "prom":
		return string(tools.IDPrometheus), nil
	case "grafana":
		return string(tools.IDGrafana), nil
	case "otel", "opentelemetry", "jaeger", "tempo":
		return string(tools.IDOpenTelemetry), nil
	default:
		return "", fmt.Errorf(
			"unknown --only component %q (want helm|argo-workflows|prometheus|grafana|opentelemetry)",
			raw,
		)
	}
}

// BuildPlan turns a detect registry into a setup plan.
func BuildPlan(reg *tools.Registry, opts Options) (Plan, error) {
	profile, err := NormalizeProfile(opts.Profile)
	if err != nil {
		return Plan{}, err
	}
	only, err := NormalizeOnly(opts.Only)
	if err != nil {
		return Plan{}, err
	}
	dryRun := opts.DryRun
	plan := Plan{
		Type:    "setup",
		Profile: profile,
		Only:    only,
		DryRun:  dryRun,
		Steps:   make([]Step, 0, 8),
		Notes: []string{
			"Plan from tools.Detect (ADR-0018). Default is dry-run.",
			"Apply with --approve / TTY: host Helm + cluster Argo/Prom stack.",
			"Wipe-class uninstalls denied. Re-check: kprompt tools · kprompt doctor",
			"Config-lane (Grafana/OTel) is never auto-written — use kprompt config set.",
		},
	}

	want, err := filterComponents(componentsForProfile(profile), only)
	if err != nil {
		return Plan{}, err
	}
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

	onlyNote := ""
	if len(only) > 0 {
		onlyNote = fmt.Sprintf(" (--only %s)", strings.Join(only, ","))
	}
	switch {
	case plan.Needed == 0:
		plan.Summary = fmt.Sprintf(
			"Profile %q%s: all %d components ready — nothing to install or configure.",
			profile, onlyNote, plan.Ready,
		)
	default:
		mode := "dry-run plan — approve/apply not run"
		if !dryRun {
			mode = "plan ready for approve/apply"
		}
		plan.Summary = fmt.Sprintf(
			"Profile %q%s: %d needed, %d ready (%s).",
			profile, onlyNote, plan.Needed, plan.Ready, mode,
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

func filterComponents(all []componentSpec, only []string) ([]componentSpec, error) {
	if len(only) == 0 {
		return all, nil
	}
	byID := map[string]componentSpec{}
	for _, c := range all {
		byID[string(c.ID)] = c
	}
	out := make([]componentSpec, 0, len(only))
	for _, id := range only {
		c, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf(
				"component %q is not in the selected profile (use a broader --profile or drop it from --only)",
				id,
			)
		}
		out = append(out, c)
	}
	return out, nil
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
				"kubectl create namespace " + DefaultArgoNamespace,
				"kubectl apply -n " + DefaultArgoNamespace + " -f " + argoWorkflowsInstallYAML,
				"# docs: https://argo-workflows.readthedocs.io/en/latest/quick-start/",
			}
		},
	}
	prom := componentSpec{
		ID: tools.IDPrometheus, Name: "Prometheus", Lane: LaneCluster, Risk: "medium",
		Action: "install-cluster",
		Commands: func(tools.Result) []string {
			return []string{
				"helm repo add " + prometheusCommunityRepoName + " " + prometheusCommunityRepoURL,
				"helm repo update " + prometheusCommunityRepoName,
				"helm install " + DefaultPrometheusRelease + " " + kubePrometheusStackChart +
					" -n " + DefaultPrometheusNamespace + " --create-namespace",
				"# then: kprompt config set tools.prometheus.url http://" + DefaultPrometheusRelease + "-kube-prometheus-stack-prometheus." + DefaultPrometheusNamespace + ".svc:9090",
				"# or skip install and point at an existing Prometheus URL",
			}
		},
		ConfigMsg: "Install kube-prometheus-stack (Helm) or configure an existing Prometheus URL after approve.",
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
	only := ""
	if len(plan.Only) > 0 {
		only = ", only=" + strings.Join(plan.Only, ",")
	}
	fmt.Fprintf(w, "Setup plan (profile=%s%s, dry-run=%v)\n", plan.Profile, only, plan.DryRun)
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
		fmt.Fprintln(w, "\nNo mutations performed yet. Apply host+cluster with --approve or TTY confirm.")
	}
	return nil
}

func sanitizeTab(s string) string {
	return strings.ReplaceAll(s, "\t", " ")
}
