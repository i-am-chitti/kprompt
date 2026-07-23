package verify

import (
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/kprompt/kprompt/internal/cluster"
	"github.com/kprompt/kprompt/internal/planner"
)

// Status values for post-apply checks (T-070).
const (
	OK      = "ok"
	Pending = "pending"
	Failed  = "failed"
	Skipped = "skipped"
)

// Check is one resource-level post-apply assertion.
type Check struct {
	Op        string `json:"op"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Status    string `json:"status"`
	Detail    string `json:"detail,omitempty"`
}

// Report is the plan-level post-apply outcome (review aid + CI field).
type Report struct {
	Status  string  `json:"status"`
	Message string  `json:"message"`
	Checks  []Check `json:"checks,omitempty"`
}

// Plan checks whether mutating actions appear to have met their goal.
// Without waiting, Deployments may be pending; that is not a hard failure.
func Plan(ctx context.Context, client kubernetes.Interface, plan planner.ExecutionPlan) Report {
	if client == nil || !plan.RequiresApproval {
		return Report{Status: Skipped, Message: "verify skipped"}
	}
	var checks []Check
	for _, a := range plan.Actions {
		if c, ok := verifyAction(ctx, client, a); ok {
			checks = append(checks, c)
		}
	}
	if len(checks) == 0 {
		return Report{Status: Skipped, Message: "no verifiable mutate targets"}
	}
	return rollup(checks)
}

func verifyAction(ctx context.Context, client kubernetes.Interface, a planner.Action) (Check, bool) {
	ns := a.Object.Namespace
	if ns == "" {
		ns = "default"
	}
	base := Check{
		Op:        string(a.Op),
		Kind:      a.Object.Kind,
		Name:      a.Object.Name,
		Namespace: ns,
	}
	if a.Object.Name == "" {
		return Check{}, false
	}
	switch a.Op {
	case planner.OpDelete:
		return verifyDeleted(ctx, client, base), true
	case planner.OpScale, planner.OpCreate, planner.OpUpdate, planner.OpRollback:
		switch strings.ToLower(a.Object.Kind) {
		case "deployment", "":
			base.Kind = "Deployment"
			return verifyDeployment(ctx, client, base, a.Replicas), true
		case "service":
			return verifyServicePresent(ctx, client, base), true
		default:
			return Check{}, false
		}
	default:
		return Check{}, false
	}
}

func verifyDeleted(ctx context.Context, client kubernetes.Interface, base Check) Check {
	var err error
	switch strings.ToLower(base.Kind) {
	case "deployment":
		_, err = client.AppsV1().Deployments(base.Namespace).Get(ctx, base.Name, metav1.GetOptions{})
	case "service":
		_, err = client.CoreV1().Services(base.Namespace).Get(ctx, base.Name, metav1.GetOptions{})
	case "pod":
		_, err = client.CoreV1().Pods(base.Namespace).Get(ctx, base.Name, metav1.GetOptions{})
	default:
		base.Status = Skipped
		base.Detail = "delete verify not supported for kind"
		return base
	}
	if apierrors.IsNotFound(err) {
		base.Status = OK
		base.Detail = "not found (deleted)"
		return base
	}
	if err != nil {
		base.Status = Failed
		base.Detail = err.Error()
		return base
	}
	base.Status = Failed
	base.Detail = "still present"
	return base
}

func verifyDeployment(ctx context.Context, client kubernetes.Interface, base Check, wantReplicas *int32) Check {
	dep, err := client.AppsV1().Deployments(base.Namespace).Get(ctx, base.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		base.Status = Failed
		base.Detail = "Deployment not found after apply"
		return base
	}
	if err != nil {
		base.Status = Failed
		base.Detail = err.Error()
		return base
	}
	desired := cluster.DesiredReplicas(dep)
	if wantReplicas != nil {
		desired = *wantReplicas
		if dep.Spec.Replicas == nil || *dep.Spec.Replicas != desired {
			base.Status = Pending
			cur := int32(0)
			if dep.Spec.Replicas != nil {
				cur = *dep.Spec.Replicas
			}
			base.Detail = fmt.Sprintf("spec.replicas %d (want %d)", cur, desired)
			return base
		}
	}
	if cluster.DeploymentRolledOut(dep) {
		base.Status = OK
		base.Detail = fmt.Sprintf("ready %d/%d", dep.Status.AvailableReplicas, desired)
		return base
	}
	base.Status = Pending
	base.Detail = fmt.Sprintf("updated=%d available=%d desired=%d",
		dep.Status.UpdatedReplicas, dep.Status.AvailableReplicas, desired)
	return base
}

func verifyServicePresent(ctx context.Context, client kubernetes.Interface, base Check) Check {
	_, err := client.CoreV1().Services(base.Namespace).Get(ctx, base.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		base.Status = Failed
		base.Detail = "Service not found after apply"
		return base
	}
	if err != nil {
		base.Status = Failed
		base.Detail = err.Error()
		return base
	}
	base.Status = OK
	base.Detail = "present"
	return base
}

func rollup(checks []Check) Report {
	failed, pending := 0, 0
	for _, c := range checks {
		switch c.Status {
		case Failed:
			failed++
		case Pending:
			pending++
		}
	}
	rep := Report{Checks: checks}
	switch {
	case failed > 0:
		rep.Status = Failed
		rep.Message = fmt.Sprintf("%d check(s) failed", failed)
	case pending > 0:
		rep.Status = Pending
		rep.Message = fmt.Sprintf("%d check(s) pending (use --wait to block on rollout)", pending)
	default:
		rep.Status = OK
		rep.Message = "goal met"
	}
	return rep
}
