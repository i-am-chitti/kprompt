package planner

import (
	"fmt"
	"strings"

	"github.com/kprompt/kprompt/internal/intent"
)

func buildCleanup(in intent.Intent) (ExecutionPlan, error) {
	scopeNS := strings.TrimSpace(in.Target.Namespace)
	if scope, ok := in.StringParam("scope"); ok && scope == "cluster" {
		scopeNS = ""
		in.Target.Namespace = ""
	}
	if in.Params == nil {
		in.Params = map[string]any{}
	}

	summary := "Cleanup cluster unused / stale resources (read-only report)"
	if scopeNS != "" {
		summary = fmt.Sprintf("Cleanup namespace %s unused / stale resources (read-only report)", scopeNS)
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
			Op:      OpCleanup,
			Backend: "cleanup",
			Object: ObjectRef{
				Kind:      in.Target.Kind,
				Namespace: scopeNS,
			},
			Diff: "scan for unused ConfigMaps/Secrets, completed Jobs, and superseded ReplicaSets; deletes require separate approval",
		}},
		Summary:          summary,
		RequiresApproval: false,
	}, nil
}
