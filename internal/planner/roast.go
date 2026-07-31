package planner

import (
	"fmt"
	"strings"

	"github.com/kprompt/kprompt/internal/intent"
)

func buildRoast(in intent.Intent) (ExecutionPlan, error) {
	scopeNS := strings.TrimSpace(in.Target.Namespace)
	if scope, ok := in.StringParam("scope"); ok && scope == "cluster" {
		scopeNS = ""
		in.Target.Namespace = ""
	}
	if in.Params == nil {
		in.Params = map[string]any{}
	}

	summary := "Roast cluster health (read-only, no LLM)"
	if scopeNS != "" {
		summary = fmt.Sprintf("Roast namespace %s health (read-only, no LLM)", scopeNS)
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
			Op:      OpRoast,
			Backend: "roast",
			Object: ObjectRef{
				Kind:      in.Target.Kind,
				Namespace: scopeNS,
			},
			Diff: "observe health score + witty verdict; never mutates",
		}},
		Summary:          summary,
		RequiresApproval: false,
	}, nil
}
