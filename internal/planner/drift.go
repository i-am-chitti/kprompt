package planner

import (
	"fmt"
	"strings"

	"github.com/kprompt/kprompt/internal/intent"
)

func buildDrift(in intent.Intent) (ExecutionPlan, error) {
	scopeNS := strings.TrimSpace(in.Target.Namespace)
	if scope, ok := in.StringParam("scope"); ok && scope == "cluster" {
		scopeNS = ""
		in.Target.Namespace = ""
	}
	if in.Params == nil {
		in.Params = map[string]any{}
	}

	summary := "Drift scan: live cluster vs GitOps desired state (read-only)"
	if scopeNS != "" {
		summary = fmt.Sprintf("Drift scan in namespace %s vs GitOps (read-only)", scopeNS)
	}
	if name := strings.TrimSpace(in.Target.Name); name != "" {
		summary = fmt.Sprintf("Drift scan for %s vs GitOps (read-only)", name)
	}

	if strings.TrimSpace(in.Target.Kind) == "" {
		if scopeNS == "" && strings.TrimSpace(in.Target.Name) == "" {
			in.Target.Kind = "Cluster"
		} else if strings.TrimSpace(in.Target.Name) == "" {
			in.Target.Kind = "Namespace"
		}
	}

	return ExecutionPlan{
		Intent: in,
		Actions: []Action{{
			Op:      OpDrift,
			Backend: "drift",
			Object: ObjectRef{
				Kind:      firstNonEmpty(in.Target.Kind, "Cluster"),
				Name:      in.Target.Name,
				Namespace: scopeNS,
			},
			Diff: "compare Flux/Argo CD sync+health to live; optional approve-gated sync plans via suggest (PR mode is T-072)",
		}},
		Summary:          summary,
		RequiresApproval: false,
	}, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
