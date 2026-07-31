package pipeline

import (
	"context"
	"fmt"
	"io"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/kprompt/kprompt/internal/agent/health"
	"github.com/kprompt/kprompt/internal/cluster"
	"github.com/kprompt/kprompt/internal/config"
	"github.com/kprompt/kprompt/internal/intent"
	"github.com/kprompt/kprompt/internal/output"
	"github.com/kprompt/kprompt/internal/planner"
	"github.com/kprompt/kprompt/internal/roast"
	"github.com/kprompt/kprompt/internal/safety"
	"github.com/kprompt/kprompt/internal/team"
	"github.com/kprompt/kprompt/internal/ui"
)

const maxRoastNamespaces = 40

// runRoastPrompt handles roast / vibe-check prompts without LLM spend.
func runRoastPrompt(ctx context.Context, cfg config.Resolved, out io.Writer, deps Deps) error {
	jsonMode := cfg.JSONOutput()
	in := intent.Intent{
		Kind: intent.KindRoast,
		Raw:  cfg.Prompt,
	}
	in = intent.NormalizeRoast(in, cfg.Prompt)
	in = intent.ApplyScope(in, intent.ScopePrefs{
		DefaultNamespace: cfg.Namespace,
		DefaultContext:   cfg.Context,
		ForceNamespace:   cfg.NamespaceFromCLI,
		ForceContext:     cfg.ContextFromCLI,
	})
	in = intent.ApplyRoastScope(in, cfg.Prompt, intent.ScopePrefs{
		DefaultNamespace: cfg.Namespace,
		ForceNamespace:   cfg.NamespaceFromCLI,
	})
	cfg.Namespace = in.Target.Namespace
	if in.Context != "" {
		cfg.Context = in.Context
	}
	resolveCfgContext(&cfg)

	plan, err := planner.Build(in)
	if err != nil {
		return err
	}
	risk := safety.EvaluatePlanWithOrg(plan, orgPolicy(deps))
	if risk.Denied {
		doc := output.FromPlan(cfg.Prompt, cfg.Context, plan, risk, false)
		if deps.OnResult != nil {
			deps.OnResult(doc)
		}
		if jsonMode {
			return output.Encode(out, doc)
		}
		ui.PrintDenied(out, risk.Message)
		return nil
	}

	client := deps.Client
	if client == nil {
		if cfg.Context != "" {
			if err := cluster.EnsureContext(cfg.Context); err != nil {
				return err
			}
		}
		clients, err := cluster.Connect(cfg.Context)
		if err != nil {
			return err
		}
		client = clients.Clientset
	}

	report, err := buildRoastReport(ctx, client, plan)
	if err != nil {
		return cluster.Friendlier(err)
	}

	doc := output.FromPlan(cfg.Prompt, cfg.Context, plan, risk, true).WithRoastResult(report)
	team.PushAuditBestEffort(ctx, auditFromPlan(cfg, plan, risk, "applied"))
	if deps.OnResult != nil {
		deps.OnResult(doc)
	}
	if jsonMode {
		return output.Encode(out, doc)
	}
	ui.PrintRoast(out, report)
	return nil
}

func buildRoastReport(ctx context.Context, client kubernetes.Interface, plan planner.ExecutionPlan) (roast.Report, error) {
	ns := strings.TrimSpace(plan.Intent.Target.Namespace)
	scope, _ := plan.Intent.StringParam("scope")
	if scope == "cluster" || ns == "" {
		list, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			return roast.Report{}, fmt.Errorf("roast list namespaces: %w", err)
		}
		snaps := make([]health.Snapshot, 0, len(list.Items))
		for i, item := range list.Items {
			if i >= maxRoastNamespaces {
				break
			}
			name := item.Name
			if name == "" {
				continue
			}
			snap := health.NewTracker(name, client).Evaluate(ctx, nil)
			snaps = append(snaps, snap)
		}
		return roast.FromFleet(snaps), nil
	}
	snap := health.NewTracker(ns, client).Evaluate(ctx, nil)
	return roast.FromSnapshot(snap), nil
}
