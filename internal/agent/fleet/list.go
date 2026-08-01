// Package fleet lists Namespace Agent / Observe surfaces across the cluster (AG-062).
package fleet

import (
	"context"
	"fmt"
	"sort"
	"strings"

	agentv1 "github.com/kprompt/kprompt/api/v1"
	"github.com/kprompt/kprompt/internal/agent/operator"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

var gvr = schema.GroupVersionResource{
	Group:    agentv1.Group,
	Version:  agentv1.Version,
	Resource: "kpromptagents",
}

// AgentRow is one fleet inventory entry.
type AgentRow struct {
	Source        string `json:"source"` // cr | deployment
	Namespace     string `json:"namespace"`
	Name          string `json:"name"`
	WatchNS       string `json:"watchNamespace,omitempty"`
	Mode          string `json:"mode,omitempty"`
	Ready         string `json:"ready,omitempty"`
	HealthScore   *int   `json:"healthScore,omitempty"`
	HealthTrend   string `json:"healthTrend,omitempty"`
	OpenIncidents int    `json:"openIncidents,omitempty"`
	LastAlert     string `json:"lastAlert,omitempty"`
	Replicas      string `json:"replicas,omitempty"`
	Note          string `json:"note,omitempty"`
}

// Inventory is the Namespace Agent fleet UX MVP (read-only).
type Inventory struct {
	APIVersion string     `json:"apiVersion"`
	Kind       string     `json:"kind"`
	Agents     []AgentRow `json:"agents"`
	Notes      []string   `json:"notes,omitempty"`
}

const (
	kindInventory = "AgentFleet"
	apiVersion    = "kprompt.io/v1"
)

// ListOptions scopes the inventory.
type ListOptions struct {
	Namespace string // empty → all namespaces
}

// List gathers KpromptAgent CRs and labeled Observe Deployments.
func List(ctx context.Context, dyn dynamic.Interface, cs kubernetes.Interface, opts ListOptions) (Inventory, error) {
	inv := Inventory{
		APIVersion: apiVersion,
		Kind:       kindInventory,
		Agents:     []AgentRow{},
	}
	ns := strings.TrimSpace(opts.Namespace)

	if dyn != nil {
		rows, note, err := listCRs(ctx, dyn, ns)
		if err != nil {
			inv.Notes = append(inv.Notes, fmt.Sprintf("KpromptAgent list: %v", err))
		} else {
			inv.Agents = append(inv.Agents, rows...)
			if note != "" {
				inv.Notes = append(inv.Notes, note)
			}
		}
	} else {
		inv.Notes = append(inv.Notes, "dynamic client missing — skipped KpromptAgent CRs")
	}

	if cs != nil {
		rows, err := listDeployments(ctx, cs, ns)
		if err != nil {
			inv.Notes = append(inv.Notes, fmt.Sprintf("Deployment list: %v", err))
		} else {
			inv.Agents = append(inv.Agents, dedupeDeployments(inv.Agents, rows)...)
		}
	}

	sort.Slice(inv.Agents, func(i, j int) bool {
		if inv.Agents[i].Namespace != inv.Agents[j].Namespace {
			return inv.Agents[i].Namespace < inv.Agents[j].Namespace
		}
		if inv.Agents[i].Source != inv.Agents[j].Source {
			return inv.Agents[i].Source < inv.Agents[j].Source
		}
		return inv.Agents[i].Name < inv.Agents[j].Name
	})
	if len(inv.Agents) == 0 && len(inv.Notes) == 0 {
		inv.Notes = append(inv.Notes, "no KpromptAgent CRs or kprompt-agent Deployments found")
	}
	return inv, nil
}

func listCRs(ctx context.Context, dyn dynamic.Interface, ns string) ([]AgentRow, string, error) {
	list, err := dyn.Resource(gvr).Namespace(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, "", err
	}
	var rows []AgentRow
	for i := range list.Items {
		cr, err := operator.FromUnstructured(&list.Items[i])
		if err != nil {
			continue
		}
		watchNS := operator.WatchNamespace(cr)
		mode := agentv1.DefaultMode(cr.Spec.Mode)
		row := AgentRow{
			Source:        "cr",
			Namespace:     cr.Namespace,
			Name:          cr.Name,
			WatchNS:       watchNS,
			Mode:          mode,
			HealthScore:   cr.Status.HealthScore,
			HealthTrend:   cr.Status.HealthTrend,
			OpenIncidents: cr.Status.OpenIncidents,
			Ready:         conditionReady(cr.Status.Conditions),
		}
		if cr.Status.LastAlert != nil && cr.Status.LastAlert.Summary != "" {
			row.LastAlert = cr.Status.LastAlert.Summary
		}
		rows = append(rows, row)
	}
	return rows, "", nil
}

func listDeployments(ctx context.Context, cs kubernetes.Interface, ns string) ([]AgentRow, error) {
	opts := metav1.ListOptions{
		LabelSelector: operator.LabelName + "=" + operator.AppName,
	}
	var items []appsv1.Deployment
	if ns == "" {
		list, err := cs.AppsV1().Deployments("").List(ctx, opts)
		if err != nil {
			return nil, err
		}
		items = list.Items
	} else {
		list, err := cs.AppsV1().Deployments(ns).List(ctx, opts)
		if err != nil {
			return nil, err
		}
		items = list.Items
	}
	rows := make([]AgentRow, 0, len(items))
	for _, d := range items {
		ready := fmt.Sprintf("%d/%d", d.Status.ReadyReplicas, deref(d.Spec.Replicas))
		rows = append(rows, AgentRow{
			Source:    "deployment",
			Namespace: d.Namespace,
			Name:      d.Name,
			WatchNS:   d.Namespace,
			Mode:      agentv1.ModeObserve,
			Replicas:  ready,
			Note:      "Helm/operator Deployment (no CR status)",
		})
	}
	return rows, nil
}

// dedupeDeployments drops Deployments already covered by a CR in the same namespace
// when the Deployment name matches kprompt-agent-<crname>.
func dedupeDeployments(existing, deps []AgentRow) []AgentRow {
	covered := map[string]struct{}{}
	for _, r := range existing {
		if r.Source != "cr" {
			continue
		}
		covered[r.Namespace+"/"+operator.AppName+"-"+r.Name] = struct{}{}
		covered[r.Namespace+"/"+r.Name] = struct{}{}
	}
	var out []AgentRow
	for _, d := range deps {
		key := d.Namespace + "/" + d.Name
		if _, ok := covered[key]; ok {
			continue
		}
		out = append(out, d)
	}
	return out
}

func conditionReady(conds []metav1.Condition) string {
	for _, c := range conds {
		if c.Type == "Ready" {
			return string(c.Status)
		}
	}
	return ""
}

func deref(p *int32) int32 {
	if p == nil {
		return 0
	}
	return *p
}

// Format renders a compact human-readable fleet table.
func Format(inv Inventory) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Namespace Agent fleet (MVP) · %d surface(s)\n", len(inv.Agents))
	if len(inv.Agents) == 0 {
		fmt.Fprintf(&b, "(empty)\n")
	}
	for _, a := range inv.Agents {
		health := "-"
		if a.HealthScore != nil {
			health = fmt.Sprintf("%d", *a.HealthScore)
			if a.HealthTrend != "" {
				health += " " + a.HealthTrend
			}
		}
		line := fmt.Sprintf("- [%s] %s/%s mode=%s watch=%s ready=%s health=%s",
			a.Source, a.Namespace, a.Name, orDash(a.Mode), orDash(a.WatchNS), orDash(a.Ready), health)
		if a.OpenIncidents > 0 {
			line += fmt.Sprintf(" open=%d", a.OpenIncidents)
		}
		if a.Replicas != "" {
			line += " replicas=" + a.Replicas
		}
		fmt.Fprintln(&b, line)
		if a.LastAlert != "" {
			fmt.Fprintf(&b, "    lastAlert: %s\n", a.LastAlert)
		}
		if a.Note != "" {
			fmt.Fprintf(&b, "    note: %s\n", a.Note)
		}
	}
	for _, n := range inv.Notes {
		fmt.Fprintf(&b, "note: %s\n", n)
	}
	fmt.Fprintf(&b, "Mutate stays off for Observe agents · AG-062")
	return strings.TrimSpace(b.String())
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
