package planner

import (
	"fmt"
	"strings"

	"github.com/kprompt/kprompt/internal/intent"
)

func buildAudit(in intent.Intent) (ExecutionPlan, error) {
	scopeNS := strings.TrimSpace(in.Target.Namespace)
	if scope, ok := in.StringParam("scope"); ok && scope == "cluster" {
		scopeNS = ""
		in.Target.Namespace = ""
	}
	if in.Params == nil {
		in.Params = map[string]any{}
	}

	summary := "Audit cluster security / hygiene (read-only)"
	if scopeNS != "" {
		summary = fmt.Sprintf("Audit namespace %s security / hygiene (read-only)", scopeNS)
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
			Op:      OpAudit,
			Backend: "audit",
			Object: ObjectRef{
				Kind:      in.Target.Kind,
				Namespace: scopeNS,
			},
			Diff: "scan Deployments/StatefulSets/DaemonSets for root, privileged, latest tags, missing resources",
		}},
		Summary:          summary,
		RequiresApproval: false,
	}, nil
}
