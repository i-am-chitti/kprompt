package ctxbuild

import (
	"context"
	"fmt"
	"strings"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	"github.com/kprompt/kprompt/internal/incident"
	"github.com/kprompt/kprompt/internal/tools/gitops"
)

const maxGitOpsEvidence = 6

// GitOpsQuerier is the optional Argo/Flux status adapter (AG-035).
type GitOpsQuerier interface {
	NamespaceStatus(ctx context.Context, namespace string) (gitops.StatusReport, error)
}

// ClusterGitOps lists Flux/Argo apps in a namespace via the dynamic client.
type ClusterGitOps struct {
	Config  *rest.Config
	Dynamic dynamic.Interface
}

// NamespaceStatus returns a read-only GitOps sync/health report for ns.
func (c *ClusterGitOps) NamespaceStatus(ctx context.Context, namespace string) (gitops.StatusReport, error) {
	if c == nil || c.Dynamic == nil || c.Config == nil {
		return gitops.StatusReport{}, fmt.Errorf("gitops: client unset")
	}
	av, err := gitops.Detect(ctx, c.Config)
	if err != nil {
		return gitops.StatusReport{}, err
	}
	return gitops.SummarizeStatusWithClient(ctx, c.Dynamic, av, gitops.StatusRequest{
		Namespace: strings.TrimSpace(namespace),
		Engine:    "auto",
	})
}

// enrichGitOps attaches GitOps EvidenceRefs when a Querier is configured (opt-in).
// Nil querier → skip (no degrade). Failed / empty CRDs → Degraded "gitops" (ADR-0016 honesty).
func (b *Builder) enrichGitOps(ctx context.Context, out *AgentContext, workload string) {
	if b == nil || b.GitOps == nil {
		return
	}
	ns := strings.TrimSpace(out.Namespace)
	if ns == "" {
		out.Degraded = appendUnique(out.Degraded, "gitops")
		return
	}
	rep, err := b.GitOps.NamespaceStatus(ctx, ns)
	if err != nil {
		out.Degraded = appendUnique(out.Degraded, "gitops")
		return
	}
	if !strings.Contains(strings.ToLower(rep.Summary), "not available") && len(rep.Apps) == 0 {
		// Controllers present but no apps in ns — still useful signal, no degrade.
		if len(rep.Notes) > 0 {
			for _, n := range rep.Notes {
				if strings.Contains(strings.ToLower(n), "not installed") ||
					strings.Contains(strings.ToLower(n), "not available") {
					out.Degraded = appendUnique(out.Degraded, "gitops")
					return
				}
			}
		}
	}
	if strings.Contains(strings.ToLower(rep.Summary), "not available") ||
		(len(rep.Apps) == 0 && len(rep.Notes) > 0 && strings.Contains(strings.ToLower(strings.Join(rep.Notes, " ")), "not installed")) {
		out.Degraded = appendUnique(out.Degraded, "gitops")
		return
	}
	if len(rep.Apps) == 0 {
		return
	}

	now := time.Now().UTC()
	wl := strings.ToLower(strings.TrimSpace(workload))
	apps := prioritizeGitOpsApps(rep.Apps, wl)
	for i, app := range apps {
		if i >= maxGitOpsEvidence {
			break
		}
		ts := now
		msg := fmt.Sprintf("%s %s/%s sync=%s health=%s",
			app.Engine, app.Namespace, app.Name,
			firstNonEmpty(app.Sync, "?"), firstNonEmpty(app.Health, "?"))
		if app.Revision != "" {
			msg += " rev=" + truncate(app.Revision, 40)
		}
		if app.Message != "" {
			msg += "; " + truncate(app.Message, 120)
		}
		reason := "sync_status"
		if strings.EqualFold(app.Sync, "OutOfSync") || strings.EqualFold(app.Sync, "False") {
			reason = "out_of_sync"
		} else if app.Health != "" && !strings.EqualFold(app.Health, "Healthy") && !strings.EqualFold(app.Health, "True") {
			reason = "unhealthy"
		}
		out.GitOps = append(out.GitOps, incident.EvidenceRef{
			Type:      incident.EvidenceGitOps,
			Reason:    reason,
			Message:   truncate(msg, 400),
			Timestamp: &ts,
			Source:    "gitops",
			URI:       firstNonEmpty(app.Revision, app.Engine+":"+app.Name),
			Resource: &incident.ResourceRef{
				Kind:      app.Kind,
				Name:      app.Name,
				Namespace: app.Namespace,
			},
		})
		if len(app.History) > 0 {
			histMsg := "deploy history: " + strings.Join(truncateSlice(app.History, 3, 40), " → ")
			out.GitOps = append(out.GitOps, incident.EvidenceRef{
				Type:      incident.EvidenceGitOps,
				Reason:    "deploy_history",
				Message:   truncate(histMsg, 300),
				Timestamp: &ts,
				Source:    "gitops",
				URI:       app.Engine + ":" + app.Name + "#history",
				Resource: &incident.ResourceRef{
					Kind:      app.Kind,
					Name:      app.Name,
					Namespace: app.Namespace,
				},
			})
		}
	}
}

func prioritizeGitOpsApps(apps []gitops.AppStatus, workloadLower string) []gitops.AppStatus {
	if workloadLower == "" || len(apps) <= 1 {
		return apps
	}
	var matched, rest []gitops.AppStatus
	for _, a := range apps {
		name := strings.ToLower(a.Name)
		if name == workloadLower || strings.Contains(name, workloadLower) || strings.Contains(workloadLower, name) {
			matched = append(matched, a)
		} else {
			rest = append(rest, a)
		}
	}
	return append(matched, rest...)
}

func truncateSlice(vals []string, n, each int) []string {
	if len(vals) > n {
		vals = vals[:n]
	}
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		out = append(out, truncate(v, each))
	}
	return out
}
