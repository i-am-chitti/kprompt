package planner

import (
	"strings"

	"github.com/kprompt/kprompt/internal/intent"
)

func buildLearn(in intent.Intent) (ExecutionPlan, error) {
	if strings.TrimSpace(in.Target.Kind) == "" {
		in.Target.Kind = "Cluster"
	}
	return ExecutionPlan{
		Intent: in,
		Actions: []Action{{
			Op:      OpLearn,
			Backend: "learn",
			Object: ObjectRef{
				Kind: in.Target.Kind,
			},
			Diff: "detect cluster tools (Helm/Linkerd/Prom/Gateway API/cert-manager/GitOps) and persist local profile",
		}},
		Summary:          "Learn cluster tool profile (read-only; local persist)",
		RequiresApproval: false,
	}, nil
}
