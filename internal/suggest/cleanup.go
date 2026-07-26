package suggest

import (
	"context"
	"fmt"
	"strings"

	"github.com/kprompt/kprompt/internal/incident"
	"github.com/kprompt/kprompt/internal/intent"
	"github.com/kprompt/kprompt/internal/planner"
)

// FromCleanup turns cleanup Investigation findings into one aggregate,
// approve-gated delete plan for completed Jobs and superseded ReplicaSets,
// plus guidance for ConfigMap/Secret orphans (never auto-deleted — false
// positives from unscanned CRD/GitOps refs are too common).
func FromCleanup(_ context.Context, inv incident.Investigation) ([]Suggestion, error) {
	var actions []planner.Action
	seenDelete := map[string]bool{}
	seenGuidance := map[string]bool{}
	var guidance []Suggestion

	for _, f := range inv.Findings {
		ref := auditResource(f)
		if ref == nil || ref.Name == "" {
			continue
		}
		switch f.Code {
		case "Cleanup.CompletedJob":
			if ref.Kind != "Job" {
				continue
			}
			key := "Job|" + ref.Namespace + "|" + ref.Name
			if seenDelete[key] {
				continue
			}
			seenDelete[key] = true
			actions = append(actions, planner.Action{
				Op: planner.OpDelete,
				Object: planner.ObjectRef{
					APIVersion: "batch/v1",
					Kind:       "Job",
					Name:       ref.Name,
					Namespace:  ref.Namespace,
				},
				Diff: fmt.Sprintf("- Job/%s -n %s (completed, stale)", ref.Name, ref.Namespace),
			})
		case "Cleanup.OldReplicaSet":
			if ref.Kind != "ReplicaSet" {
				continue
			}
			key := "ReplicaSet|" + ref.Namespace + "|" + ref.Name
			if seenDelete[key] {
				continue
			}
			seenDelete[key] = true
			actions = append(actions, planner.Action{
				Op: planner.OpDelete,
				Object: planner.ObjectRef{
					APIVersion: "apps/v1",
					Kind:       "ReplicaSet",
					Name:       ref.Name,
					Namespace:  ref.Namespace,
				},
				Diff: fmt.Sprintf("- ReplicaSet/%s -n %s (superseded, 0 replicas)", ref.Name, ref.Namespace),
			})
		case "Cleanup.UnusedConfigMap", "Cleanup.UnusedSecret":
			addAuditGuidance(&guidance, seenGuidance, f.Code,
				cleanupGuidanceTitle(f.Code),
				fmt.Sprintf("describe %s/%s", strings.ToLower(ref.Kind), ref.Name),
				cleanupGuidanceSummary(f.Code))
		default:
			addAuditGuidance(&guidance, seenGuidance, f.Code,
				"Review cleanup candidate",
				fmt.Sprintf("describe %s", ref.Name),
				f.Message)
		}
	}

	var out []Suggestion
	if len(actions) > 0 {
		plan := &planner.ExecutionPlan{
			Intent: intent.Intent{
				Kind:   intent.KindDelete,
				Target: intent.Target{Kind: "Job", Namespace: inv.Namespace},
				Params: map[string]any{"reason": "Cleanup"},
			},
			Actions:          actions,
			Summary:          fmt.Sprintf("Delete %d stale Job/ReplicaSet cleanup candidate(s)", len(actions)),
			RequiresApproval: true,
		}
		out = append(out, Suggestion{
			Code:    "Cleanup.Delete",
			Title:   "Delete stale Jobs / ReplicaSets",
			Prompt:  "cleanup delete stale jobs and replicasets",
			Plan:    plan,
			Summary: plan.Summary,
		})
	}
	out = append(out, guidance...)
	return out, nil
}

func cleanupGuidanceTitle(code string) string {
	switch code {
	case "Cleanup.UnusedConfigMap":
		return "Review unused ConfigMap"
	case "Cleanup.UnusedSecret":
		return "Review unused Secret"
	default:
		return "Review cleanup candidate"
	}
}

func cleanupGuidanceSummary(code string) string {
	switch code {
	case "Cleanup.UnusedConfigMap":
		return "ConfigMaps are never auto-deleted — CRD/GitOps refs may be unscanned; confirm then delete manually"
	case "Cleanup.UnusedSecret":
		return "Secrets are never auto-deleted — false positives are common; confirm then delete manually"
	default:
		return "Review the finding before deleting"
	}
}
