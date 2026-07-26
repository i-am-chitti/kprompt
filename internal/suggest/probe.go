package suggest

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"

	"github.com/kprompt/kprompt/internal/cluster"
	"github.com/kprompt/kprompt/internal/intent"
	"github.com/kprompt/kprompt/internal/planner"
)

// suggestProbe emits a conservative Deployment patch that relaxes probe timing.
// Prefer fixing the app health endpoint; this only buys startup headroom.
func suggestProbe(ctx context.Context, client kubernetes.Interface, rep cluster.ExplainReport, f cluster.Finding) (*Suggestion, error) {
	guidance := &Suggestion{
		Code:    "ProbeFailure",
		Title:   "Inspect failing probe",
		Prompt:  fmt.Sprintf(`describe %s`, rep.Target),
		Summary: "Check readiness/liveness endpoint health; a timing bump is only a stopgap",
	}
	dep, container, err := resolveDeploymentContainer(ctx, client, rep, f.Container)
	if err != nil || dep == nil {
		return guidance, nil
	}
	idx := containerIndex(dep, container)
	if idx < 0 {
		idx = 0
		container = dep.Spec.Template.Spec.Containers[0].Name
	}
	probeKind := probeKindFromFinding(f)
	patched := dep.DeepCopy()
	c := &patched.Spec.Template.Spec.Containers[idx]
	var probe *corev1.Probe
	switch probeKind {
	case "liveness":
		if c.LivenessProbe == nil {
			return guidance, nil
		}
		probe = c.LivenessProbe
	default:
		if c.ReadinessProbe != nil {
			probe = c.ReadinessProbe
			probeKind = "readiness"
		} else if c.LivenessProbe != nil {
			probe = c.LivenessProbe
			probeKind = "liveness"
		} else {
			return guidance, nil
		}
	}
	oldDelay, oldThresh := probe.InitialDelaySeconds, probe.FailureThreshold
	relaxProbe(probe)
	patched.TypeMeta = metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"}
	raw, err := yaml.Marshal(patched)
	if err != nil {
		return nil, err
	}
	diff := fmt.Sprintf("~ Deployment/%s (update)\n  container: %s\n  %sProbe.initialDelaySeconds: %d → %d\n  %sProbe.failureThreshold: %d → %d",
		dep.Name, container, probeKind, oldDelay, probe.InitialDelaySeconds, probeKind, oldThresh, probe.FailureThreshold)
	plan := &planner.ExecutionPlan{
		Intent: intent.Intent{
			Kind: intent.KindPatch,
			Target: intent.Target{
				Name:      dep.Name,
				Namespace: dep.Namespace,
				Kind:      "Deployment",
			},
			Params: map[string]any{
				"reason":    "ProbeFailure",
				"container": container,
				"probe":     probeKind,
			},
		},
		Actions: []planner.Action{{
			Op: planner.OpUpdate,
			Object: planner.ObjectRef{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       dep.Name,
				Namespace:  dep.Namespace,
			},
			Manifest: string(raw),
			Diff:     diff,
		}},
		Summary: fmt.Sprintf("Relax %s probe on Deployment/%s container %s (initialDelay %d→%d, failureThreshold %d→%d)",
			probeKind, dep.Name, container, oldDelay, probe.InitialDelaySeconds, oldThresh, probe.FailureThreshold),
		RequiresApproval: true,
	}
	return &Suggestion{
		Code:    "ProbeFailure",
		Title:   "Relax probe timing",
		Prompt:  fmt.Sprintf(`relax %s probe on %s`, probeKind, dep.Name),
		Plan:    plan,
		Summary: plan.Summary,
	}, nil
}

func probeKindFromFinding(f cluster.Finding) string {
	blob := strings.ToLower(f.Code + " " + f.Message)
	if strings.Contains(blob, "liveness") {
		return "liveness"
	}
	return "readiness"
}

// relaxProbe doubles initialDelay (min +5s, floor 10s) and raises failureThreshold (+2, min 3).
func relaxProbe(p *corev1.Probe) {
	if p.InitialDelaySeconds <= 0 {
		p.InitialDelaySeconds = 10
	} else {
		next := p.InitialDelaySeconds * 2
		if next < p.InitialDelaySeconds+5 {
			next = p.InitialDelaySeconds + 5
		}
		p.InitialDelaySeconds = next
	}
	if p.FailureThreshold <= 0 {
		p.FailureThreshold = 3
	} else {
		p.FailureThreshold += 2
	}
}
