package planner

import (
	"fmt"
	"strings"

	"github.com/kprompt/kprompt/internal/intent"
)

func buildScore(in intent.Intent) (ExecutionPlan, error) {
	scopeNS := strings.TrimSpace(in.Target.Namespace)
	if scope, ok := in.StringParam("scope"); ok && scope == "cluster" {
		scopeNS = ""
		in.Target.Namespace = ""
	}
	if in.Params == nil {
		in.Params = map[string]any{}
	}

	summary := "Scorecard cluster reliability / security / cost (read-only)"
	if scopeNS != "" {
		summary = fmt.Sprintf("Scorecard namespace %s reliability / security / cost (read-only)", scopeNS)
	}

	if strings.TrimSpace(in.Target.Kind) == "" {
		if scopeNS == "" {
			in.Target.Kind = "Cluster"
		} else {
			in.Target.Kind = "Namespace"
		}
	}

	return ExecutionPlan{
		Intent: in,
		Actions: []Action{{
			Op:      OpScore,
			Backend: "score",
			Object: ObjectRef{
				Kind:      in.Target.Kind,
				Namespace: scopeNS,
			},
			Diff: "rollup audit + optimize inventory/idle/rightsizing/HPA into a scorecard; cost skipped without Prometheus",
		}},
		Summary:          summary,
		RequiresApproval: false,
	}, nil
}
