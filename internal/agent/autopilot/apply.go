package autopilot

import (
	"context"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"

	"github.com/kprompt/kprompt/internal/executor"
	"github.com/kprompt/kprompt/internal/planner"
	"github.com/kprompt/kprompt/internal/safety"
)

// ApplyProposal mutates the cluster only when Policy is policyAuto + Apply (AG-042).
// Always re-checks allowlist, hard-deny, Safety, and named target. Audits applied|failed.
func (e *Engine) ApplyProposal(ctx context.Context, client kubernetes.Interface, prop Proposal) (*Proposal, error) {
	if e == nil {
		return nil, fmt.Errorf("autopilot: engine is nil")
	}
	pol := e.Policy
	pol.Normalize()
	out := prop

	if !pol.PolicyAuto() {
		out.Decision = DecisionDenied
		out.Reason = "hard-deny: apply requires mode=policyAuto and apply=true (ADR-0015)"
		out.Applied = false
		_ = e.audit(out)
		return &out, fmt.Errorf("%s", out.Reason)
	}
	if denied, why := HardDenyAction(prop.ActionID); denied {
		out.Decision = DecisionDenied
		out.Reason = why
		_ = e.audit(out)
		return &out, fmt.Errorf("%s", why)
	}
	if denied, why := HardDenyPlanText(prop.Plan.Summary, prop.Plan.Steps); denied {
		out.Decision = DecisionDenied
		out.Reason = why
		_ = e.audit(out)
		return &out, fmt.Errorf("%s", why)
	}
	if dec, reason := EvaluateAction(pol, prop.ActionID); dec == DecisionDenied {
		out.Decision = DecisionDenied
		out.Reason = reason
		_ = e.audit(out)
		return &out, fmt.Errorf("%s", reason)
	}
	if prop.Confidence < pol.MinConfidence {
		out.Decision = DecisionDenied
		out.Reason = fmt.Sprintf("confidence %.2f below floor %.2f", prop.Confidence, pol.MinConfidence)
		_ = e.audit(out)
		return &out, fmt.Errorf("%s", out.Reason)
	}
	ns := strings.TrimSpace(prop.Namespace)
	name := strings.TrimSpace(prop.TargetName)
	if ns == "" || name == "" {
		out.Decision = DecisionDenied
		out.Reason = "hard-deny: namespace and named target are required"
		_ = e.audit(out)
		return &out, fmt.Errorf("%s", out.Reason)
	}

	// Safety on synthesized prompt (ADR-0003).
	prompt := prop.Plan.Summary + " " + strings.Join(prop.Plan.Steps, " ")
	if sr := safety.CheckPrompt(prompt); sr.Denied {
		out.Decision = DecisionDenied
		out.Reason = "safety: " + sr.Message
		_ = e.audit(out)
		return &out, fmt.Errorf("%s", out.Reason)
	}

	if client == nil {
		return nil, fmt.Errorf("autopilot: kubernetes client is nil")
	}

	var err error
	switch prop.ActionID {
	case ActionRestartDeployment:
		err = restartDeployment(ctx, client, ns, name)
	default:
		var plan planner.ExecutionPlan
		plan, err = proposalToPlan(prop)
		if err != nil {
			out.Decision = DecisionFailed
			out.Reason = err.Error()
			_ = e.audit(out)
			return &out, err
		}
		runner := &executor.Runner{Client: client}
		err = runner.Apply(ctx, plan)
	}
	if err != nil {
		out.Decision = DecisionFailed
		out.Reason = err.Error()
		out.Applied = false
		_ = e.audit(out)
		return &out, err
	}

	out.Decision = DecisionApplied
	out.Applied = true
	out.Reason = "applied under policyAuto (ADR-0015); audited"
	_ = e.audit(out)
	return &out, nil
}

func proposalToPlan(prop Proposal) (planner.ExecutionPlan, error) {
	ns := prop.Namespace
	name := prop.TargetName
	switch prop.ActionID {
	case ActionRollbackFailedRollout:
		return planner.ExecutionPlan{
			Summary: prop.Plan.Summary,
			Actions: []planner.Action{{
				Backend: "kubernetes",
				Op:      planner.OpRollback,
				Object:  planner.ObjectRef{Kind: "Deployment", Name: name, Namespace: ns},
			}},
		}, nil
	case ActionScaleDeployment:
		if prop.Replicas == nil {
			return planner.ExecutionPlan{}, fmt.Errorf("scaleDeployment requires replicas")
		}
		rep := *prop.Replicas
		return planner.ExecutionPlan{
			Summary: prop.Plan.Summary,
			Actions: []planner.Action{{
				Backend:  "kubernetes",
				Op:       planner.OpScale,
				Replicas: &rep,
				Object:   planner.ObjectRef{Kind: "Deployment", Name: name, Namespace: ns},
			}},
		}, nil
	case ActionEvictPod:
		return planner.ExecutionPlan{
			Summary: prop.Plan.Summary,
			Actions: []planner.Action{{
				Backend: "kubernetes",
				Op:      planner.OpDelete,
				Object:  planner.ObjectRef{Kind: "Pod", Name: name, Namespace: ns},
			}},
		}, nil
	default:
		return planner.ExecutionPlan{}, fmt.Errorf("unsupported action %q", prop.ActionID)
	}
}

func restartDeployment(ctx context.Context, client kubernetes.Interface, ns, name string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		dep, err := client.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if dep.Spec.Template.Annotations == nil {
			dep.Spec.Template.Annotations = map[string]string{}
		}
		dep.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().UTC().Format(time.RFC3339)
		_, err = client.AppsV1().Deployments(ns).Update(ctx, dep, metav1.UpdateOptions{})
		return err
	})
}
