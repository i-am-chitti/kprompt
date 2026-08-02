// Package search runs structured NL inventory queries (S-010).
//
// MVP: match a query term against workload/Service names, labels, annotations,
// images, and env/command/args. Not a CEL/SQL engine.
package search

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/kprompt/kprompt/internal/cluster"
)

const (
	TypeSearch = "SearchReport"
)

// Request scopes an inventory search.
type Request struct {
	Namespace string // empty = cluster-wide
	Prompt    string
	Query     string // required match term (e.g. "redis")
	Kind      string // Deployment|StatefulSet|DaemonSet|Pod|Service|"" (workloads)
	Match     string // image|env|label|name|annotation|all|""
}

// Hit is one matching resource with the field that matched.
type Hit struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Field     string `json:"field"` // image|env|label|name|annotation|command
	Detail    string `json:"detail"`
}

// Report is the typed JSON / table payload for search.
type Report struct {
	Type      string   `json:"type"`
	Query     string   `json:"query"`
	Kind      string   `json:"kind,omitempty"`
	Match     string   `json:"match,omitempty"`
	Namespace string   `json:"namespace,omitempty"`
	Summary   string   `json:"summary"`
	Hits      []Hit    `json:"hits"`
	Degraded  []string `json:"degraded,omitempty"`
}

// Analyzer lists inventory and filters by the typed query.
type Analyzer struct {
	Client kubernetes.Interface
}

// Run executes the inventory search.
func (a *Analyzer) Run(ctx context.Context, req Request) (Report, error) {
	if a == nil || a.Client == nil {
		return Report{}, fmt.Errorf("search: client required")
	}
	q := strings.TrimSpace(req.Query)
	if q == "" {
		return Report{}, fmt.Errorf("search: query term required (e.g. redis)")
	}
	ns := strings.TrimSpace(req.Namespace)
	match := normalizeMatch(req.Match)
	kindFilter := normalizeKindFilter(req.Kind)

	out := Report{
		Type:      TypeSearch,
		Query:     q,
		Kind:      kindFilter,
		Match:     match,
		Namespace: ns,
		Hits:      nil,
	}

	hits, warnings, err := a.collect(ctx, ns, q, kindFilter, match)
	if err != nil {
		return Report{}, err
	}
	out.Degraded = warnings
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Namespace != hits[j].Namespace {
			return hits[i].Namespace < hits[j].Namespace
		}
		if hits[i].Kind != hits[j].Kind {
			return hits[i].Kind < hits[j].Kind
		}
		if hits[i].Name != hits[j].Name {
			return hits[i].Name < hits[j].Name
		}
		return hits[i].Field < hits[j].Field
	})
	out.Hits = hits

	scope := "cluster-wide"
	if ns != "" {
		scope = "namespace " + ns
	}
	kindLabel := "workloads"
	if kindFilter != "" {
		kindLabel = kindFilter + "(s)"
	}
	out.Summary = fmt.Sprintf(
		"%d hit(s) for %q across %s (%s)",
		len(hits), q, kindLabel, scope,
	)
	if len(hits) == 0 {
		out.Summary += "; nothing matched MVP inventory fields"
	}
	return out, nil
}

func (a *Analyzer) collect(ctx context.Context, ns, query, kindFilter, match string) ([]Hit, []string, error) {
	var (
		hits     []Hit
		warnings []string
		opts     = metav1.ListOptions{Limit: cluster.DefaultReadLimit}
	)
	needle := strings.ToLower(query)

	want := func(k string) bool {
		if kindFilter == "" {
			switch k {
			case "Deployment", "StatefulSet", "DaemonSet":
				return true
			default:
				return false
			}
		}
		return strings.EqualFold(kindFilter, k)
	}

	if want("Deployment") {
		list, err := a.Client.AppsV1().Deployments(ns).List(ctx, opts)
		switch {
		case apierrors.IsForbidden(err):
			warnings = append(warnings, "deployments")
		case err != nil:
			return nil, nil, fmt.Errorf("list deployments: %w", err)
		default:
			for i := range list.Items {
				d := &list.Items[i]
				hits = append(hits, matchWorkload("Deployment", d.Namespace, d.Name, d.Labels, d.Annotations, d.Spec.Template, needle, match)...)
			}
		}
	}
	if want("StatefulSet") {
		list, err := a.Client.AppsV1().StatefulSets(ns).List(ctx, opts)
		switch {
		case apierrors.IsForbidden(err):
			warnings = append(warnings, "statefulsets")
		case err != nil:
			return nil, nil, fmt.Errorf("list statefulsets: %w", err)
		default:
			for i := range list.Items {
				s := &list.Items[i]
				hits = append(hits, matchWorkload("StatefulSet", s.Namespace, s.Name, s.Labels, s.Annotations, s.Spec.Template, needle, match)...)
			}
		}
	}
	if want("DaemonSet") {
		list, err := a.Client.AppsV1().DaemonSets(ns).List(ctx, opts)
		switch {
		case apierrors.IsForbidden(err):
			warnings = append(warnings, "daemonsets")
		case err != nil:
			return nil, nil, fmt.Errorf("list daemonsets: %w", err)
		default:
			for i := range list.Items {
				d := &list.Items[i]
				hits = append(hits, matchWorkload("DaemonSet", d.Namespace, d.Name, d.Labels, d.Annotations, d.Spec.Template, needle, match)...)
			}
		}
	}
	if want("Pod") {
		list, err := a.Client.CoreV1().Pods(ns).List(ctx, opts)
		switch {
		case apierrors.IsForbidden(err):
			warnings = append(warnings, "pods")
		case err != nil:
			return nil, nil, fmt.Errorf("list pods: %w", err)
		default:
			for i := range list.Items {
				p := &list.Items[i]
				tpl := corev1.PodTemplateSpec{ObjectMeta: p.ObjectMeta, Spec: p.Spec}
				hits = append(hits, matchWorkload("Pod", p.Namespace, p.Name, p.Labels, p.Annotations, tpl, needle, match)...)
			}
		}
	}
	if want("Service") {
		list, err := a.Client.CoreV1().Services(ns).List(ctx, opts)
		switch {
		case apierrors.IsForbidden(err):
			warnings = append(warnings, "services")
		case err != nil:
			return nil, nil, fmt.Errorf("list services: %w", err)
		default:
			for i := range list.Items {
				hits = append(hits, matchService(&list.Items[i], needle, match)...)
			}
		}
	}

	// Deduplicate identical kind/name/ns/field/detail rows.
	return uniqueHits(hits), warnings, nil
}

func matchWorkload(kind, ns, name string, labels, ann map[string]string, tpl corev1.PodTemplateSpec, needle, match string) []Hit {
	var hits []Hit
	base := Hit{Kind: kind, Name: name, Namespace: ns}

	if fieldEnabled(match, "name") && containsFold(name, needle) {
		h := base
		h.Field = "name"
		h.Detail = name
		hits = append(hits, h)
	}
	if fieldEnabled(match, "label") {
		for k, v := range labels {
			if containsFold(k, needle) || containsFold(v, needle) {
				h := base
				h.Field = "label"
				h.Detail = k + "=" + v
				hits = append(hits, h)
			}
		}
		for k, v := range tpl.Labels {
			if containsFold(k, needle) || containsFold(v, needle) {
				h := base
				h.Field = "label"
				h.Detail = "template:" + k + "=" + v
				hits = append(hits, h)
			}
		}
	}
	if fieldEnabled(match, "annotation") {
		for k, v := range ann {
			if containsFold(k, needle) || containsFold(v, needle) {
				h := base
				h.Field = "annotation"
				h.Detail = truncate(k+"="+v, 120)
				hits = append(hits, h)
			}
		}
	}

	containers := append([]corev1.Container{}, tpl.Spec.Containers...)
	containers = append(containers, tpl.Spec.InitContainers...)
	for _, c := range containers {
		if fieldEnabled(match, "image") && containsFold(c.Image, needle) {
			h := base
			h.Field = "image"
			h.Detail = c.Name + "=" + c.Image
			hits = append(hits, h)
		}
		if fieldEnabled(match, "env") {
			for _, e := range c.Env {
				if containsFold(e.Name, needle) || containsFold(e.Value, needle) {
					h := base
					h.Field = "env"
					detail := e.Name
					if e.Value != "" {
						detail += "=" + truncate(e.Value, 80)
					} else if e.ValueFrom != nil {
						detail += "=(fromRef)"
					}
					h.Detail = c.Name + ":" + detail
					hits = append(hits, h)
				}
			}
			for _, ef := range c.EnvFrom {
				if ef.ConfigMapRef != nil && containsFold(ef.ConfigMapRef.Name, needle) {
					h := base
					h.Field = "env"
					h.Detail = c.Name + ":configMapRef=" + ef.ConfigMapRef.Name
					hits = append(hits, h)
				}
				if ef.SecretRef != nil && containsFold(ef.SecretRef.Name, needle) {
					h := base
					h.Field = "env"
					h.Detail = c.Name + ":secretRef=" + ef.SecretRef.Name
					hits = append(hits, h)
				}
			}
		}
		if fieldEnabled(match, "command") {
			for _, part := range append(append([]string{}, c.Command...), c.Args...) {
				if containsFold(part, needle) {
					h := base
					h.Field = "command"
					h.Detail = c.Name + ":" + truncate(part, 100)
					hits = append(hits, h)
					break
				}
			}
		}
	}
	return hits
}

func matchService(s *corev1.Service, needle, match string) []Hit {
	var hits []Hit
	base := Hit{Kind: "Service", Name: s.Name, Namespace: s.Namespace}
	if fieldEnabled(match, "name") && containsFold(s.Name, needle) {
		h := base
		h.Field = "name"
		h.Detail = s.Name
		hits = append(hits, h)
	}
	if fieldEnabled(match, "label") {
		for k, v := range s.Labels {
			if containsFold(k, needle) || containsFold(v, needle) {
				h := base
				h.Field = "label"
				h.Detail = k + "=" + v
				hits = append(hits, h)
			}
		}
		for k, v := range s.Spec.Selector {
			if containsFold(k, needle) || containsFold(v, needle) {
				h := base
				h.Field = "label"
				h.Detail = "selector:" + k + "=" + v
				hits = append(hits, h)
			}
		}
	}
	if fieldEnabled(match, "annotation") {
		for k, v := range s.Annotations {
			if containsFold(k, needle) || containsFold(v, needle) {
				h := base
				h.Field = "annotation"
				h.Detail = truncate(k+"="+v, 120)
				hits = append(hits, h)
			}
		}
	}
	return hits
}

func fieldEnabled(match, field string) bool {
	if match == "" || match == "all" {
		return true
	}
	return match == field
}

func normalizeMatch(m string) string {
	m = strings.ToLower(strings.TrimSpace(m))
	switch m {
	case "", "all", "image", "env", "label", "name", "annotation", "command":
		if m == "" {
			return "all"
		}
		return m
	default:
		return "all"
	}
}

func normalizeKindFilter(k string) string {
	k = strings.TrimSpace(k)
	switch strings.ToLower(k) {
	case "", "workload", "workloads", "cluster", "namespace":
		return ""
	case "deploy", "deployment", "deployments":
		return "Deployment"
	case "sts", "statefulset", "statefulsets":
		return "StatefulSet"
	case "ds", "daemonset", "daemonsets":
		return "DaemonSet"
	case "po", "pod", "pods":
		return "Pod"
	case "svc", "service", "services":
		return "Service"
	default:
		// Preserve already-canonical Kind.
		if k == "Deployment" || k == "StatefulSet" || k == "DaemonSet" || k == "Pod" || k == "Service" {
			return k
		}
		return ""
	}
}

func containsFold(hay, needle string) bool {
	return needle != "" && strings.Contains(strings.ToLower(hay), needle)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func uniqueHits(in []Hit) []Hit {
	seen := map[string]struct{}{}
	out := make([]Hit, 0, len(in))
	for _, h := range in {
		key := h.Namespace + "|" + h.Kind + "|" + h.Name + "|" + h.Field + "|" + h.Detail
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, h)
	}
	return out
}
