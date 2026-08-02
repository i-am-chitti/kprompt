package planner

import (
	"fmt"
	"strings"

	"github.com/kprompt/kprompt/internal/intent"
)

func buildSearch(in intent.Intent) (ExecutionPlan, error) {
	scopeNS := strings.TrimSpace(in.Target.Namespace)
	if scope, ok := in.StringParam("scope"); ok && scope == "cluster" {
		scopeNS = ""
		in.Target.Namespace = ""
	}
	if in.Params == nil {
		in.Params = map[string]any{}
	}
	query, _ := in.StringParam("query")
	query = strings.TrimSpace(query)
	if query == "" {
		return ExecutionPlan{}, fmt.Errorf("search requires params.query (e.g. redis)")
	}

	kind := strings.TrimSpace(in.Target.Kind)
	if kind == "" || strings.EqualFold(kind, "Cluster") || strings.EqualFold(kind, "Namespace") {
		kind = "Deployment"
		in.Target.Kind = kind
	}

	summary := fmt.Sprintf("Search inventory for %q (read-only)", query)
	if scopeNS != "" {
		summary = fmt.Sprintf("Search namespace %s inventory for %q (read-only)", scopeNS, query)
	}
	if match, ok := in.StringParam("match"); ok && match != "" && match != "all" {
		summary += fmt.Sprintf(" [match=%s]", match)
	}

	return ExecutionPlan{
		Intent: in,
		Actions: []Action{{
			Op:      OpSearch,
			Backend: "search",
			Object: ObjectRef{
				Kind:      kind,
				Namespace: scopeNS,
			},
			Diff: fmt.Sprintf("match %q against names/labels/images/env (not a SQL/CEL engine)", query),
		}},
		Summary:          summary,
		RequiresApproval: false,
	}, nil
}
