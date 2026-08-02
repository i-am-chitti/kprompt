package planner

import (
	"fmt"
	"strings"

	"github.com/kprompt/kprompt/internal/intent"
)

func buildArchitecture(in intent.Intent) (ExecutionPlan, error) {
	scopeNS := strings.TrimSpace(in.Target.Namespace)
	if scope, ok := in.StringParam("scope"); ok && scope == "cluster" {
		scopeNS = ""
		in.Target.Namespace = ""
	}
	if in.Params == nil {
		in.Params = map[string]any{}
	}

	summary := "Explain cluster architecture (read-only narrative)"
	if scopeNS != "" {
		summary = fmt.Sprintf("Explain namespace %s architecture (read-only narrative)", scopeNS)
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
			Op:      OpArchitecture,
			Backend: "architecture",
			Object: ObjectRef{
				Kind:      in.Target.Kind,
				Namespace: scopeNS,
			},
			Diff: "narrate learn profile + service graph + heuristic deps; honest when profile thin",
		}},
		Summary:          summary,
		RequiresApproval: false,
	}, nil
}
