package autopilot

import (
	"fmt"
	"strings"

	"github.com/kprompt/kprompt/internal/agent/ctxbuild"
)

func detectAction(agentCtx ctxbuild.AgentContext) (action, kind, name string, replicas *int32, ok bool) {
	blob := strings.ToLower(agentCtx.Incident.Summary + " " + agentCtx.Incident.RootCause + " " + agentCtx.Incident.Recommendation)
	for _, e := range agentCtx.Incident.Evidence {
		blob += " " + strings.ToLower(e.Reason+" "+e.Message)
	}
	for _, e := range agentCtx.RecentEvents {
		blob += " " + strings.ToLower(e.Reason+" "+e.Message)
	}

	kind, name = resolveTarget(agentCtx)
	if name == "" {
		return "", "", "", nil, false
	}

	// Priority: rollback → restart → evict → scale.
	// Rollback requires explicit rollout-failure signals (not mere CrashLoop).
	failedRollout := strings.Contains(blob, "progressdeadline") ||
		(strings.Contains(blob, "rollout") && (strings.Contains(blob, "failed") || strings.Contains(blob, "timed out") || strings.Contains(blob, "timeout")))
	if failedRollout {
		return ActionRollbackFailedRollout, "Deployment", trimPodToDeploy(name), nil, true
	}

	restartish := strings.Contains(blob, "crashloop") || strings.Contains(blob, "oom") ||
		strings.Contains(blob, "imagepull") || strings.Contains(blob, "backoff")
	if restartish {
		return ActionRestartDeployment, "Deployment", trimPodToDeploy(name), nil, true
	}

	evictish := strings.Contains(blob, "node not ready") || strings.Contains(blob, "nodenotready") ||
		strings.Contains(blob, "diskpressure") || strings.Contains(blob, "memorypressure") ||
		strings.Contains(blob, "pidpressure") || strings.Contains(blob, "evict")
	if evictish {
		pod := name
		if agentCtx.Pod != nil && agentCtx.Pod.Name != "" {
			pod = agentCtx.Pod.Name
		} else if agentCtx.Target != nil && strings.EqualFold(agentCtx.Target.Kind, "Pod") {
			pod = agentCtx.Target.Name
		}
		return ActionEvictPod, "Pod", pod, nil, true
	}

	scaleish := strings.Contains(blob, "scale up") || strings.Contains(blob, "scale down") ||
		strings.Contains(blob, "scale deployment") ||
		(strings.Contains(blob, "replicas") && (strings.Contains(blob, "zero") || strings.Contains(blob, "under-repl")))
	if scaleish && agentCtx.Deployment != nil {
		var want int32 = 1
		if agentCtx.Deployment.DesiredReplicas > 0 {
			want = agentCtx.Deployment.DesiredReplicas
		}
		if strings.Contains(blob, "scale up") || strings.Contains(blob, "under-repl") {
			want = agentCtx.Deployment.DesiredReplicas + 1
			if want < 1 {
				want = 1
			}
			if want > 20 {
				want = 20 // safety cap
			}
		}
		if strings.Contains(blob, "scale down") && agentCtx.Deployment.DesiredReplicas > 1 {
			want = agentCtx.Deployment.DesiredReplicas - 1
		}
		return ActionScaleDeployment, "Deployment", trimPodToDeploy(name), &want, true
	}

	return "", "", "", nil, false
}

func resolveTarget(agentCtx ctxbuild.AgentContext) (kind, name string) {
	kind = "Deployment"
	if agentCtx.Deployment != nil {
		name = agentCtx.Deployment.Name
	}
	if agentCtx.Target != nil && agentCtx.Target.Name != "" {
		name = firstNonEmpty(name, agentCtx.Target.Name)
		if agentCtx.Target.Kind != "" {
			kind = agentCtx.Target.Kind
		}
	}
	if name == "" && agentCtx.Incident.PrimaryResource != nil {
		name = agentCtx.Incident.PrimaryResource.Name
		if agentCtx.Incident.PrimaryResource.Kind != "" {
			kind = agentCtx.Incident.PrimaryResource.Kind
		}
	}
	if name == "" && agentCtx.Pod != nil {
		name = agentCtx.Pod.Name
		kind = "Pod"
	}
	return kind, name
}

func trimPodToDeploy(name string) string {
	parts := strings.Split(name, "-")
	if len(parts) >= 3 {
		return strings.Join(parts[:len(parts)-2], "-")
	}
	return name
}

func planFor(action, ns, name string, replicas *int32) PlanBody {
	switch action {
	case ActionRollbackFailedRollout:
		return PlanBody{
			Summary: fmt.Sprintf("Rollback Deployment/%s in %s after failed rollout", name, ns),
			Steps: []string{
				fmt.Sprintf("kubectl -n %s rollout undo deployment/%s", ns, name),
				fmt.Sprintf("kubectl -n %s rollout status deployment/%s", ns, name),
			},
		}
	case ActionRestartDeployment:
		return PlanBody{
			Summary: fmt.Sprintf("Restart Deployment/%s in %s (rollout restart)", name, ns),
			Steps: []string{
				fmt.Sprintf("kubectl -n %s rollout restart deployment/%s", ns, name),
				fmt.Sprintf("kubectl -n %s rollout status deployment/%s", ns, name),
			},
		}
	case ActionScaleDeployment:
		n := int32(1)
		if replicas != nil {
			n = *replicas
		}
		return PlanBody{
			Summary: fmt.Sprintf("Scale Deployment/%s in %s to %d replicas", name, ns, n),
			Steps: []string{
				fmt.Sprintf("kubectl -n %s scale deployment/%s --replicas=%d", ns, name, n),
			},
		}
	case ActionEvictPod:
		return PlanBody{
			Summary: fmt.Sprintf("Evict Pod/%s in %s (graceful delete)", name, ns),
			Steps: []string{
				fmt.Sprintf("kubectl -n %s delete pod/%s --grace-period=30", ns, name),
			},
		}
	default:
		return PlanBody{Summary: "unknown", Steps: nil}
	}
}

func enrichExplain(p *Proposal, action, ns, name string) {
	if p == nil {
		return
	}
	switch action {
	case ActionRollbackFailedRollout:
		p.Why = "Deployment rollout failed or timed out; prior revision is likely healthier"
		p.ExpectedImpact = "Traffic returns to the last successful ReplicaSet"
		p.Rollback = fmt.Sprintf("kubectl -n %s rollout undo deployment/%s", ns, name)
		p.Risk = firstNonEmpty(p.Risk, "medium")
	case ActionRestartDeployment:
		p.Why = "Workload is crashlooping / OOM / image-pull failing; a restart may clear a bad pod state"
		p.ExpectedImpact = "New pods scheduled; brief disruption possible"
		p.Rollback = "No cluster object change beyond pod replacement; redeploy previous image if needed"
		p.Risk = firstNonEmpty(p.Risk, "medium")
	case ActionScaleDeployment:
		p.Why = "Replica count appears wrong for current demand or availability"
		p.ExpectedImpact = "Replica count changes; capacity / cost impact"
		p.Rollback = "Scale back to the previous replica count"
		p.Risk = firstNonEmpty(p.Risk, "medium")
	case ActionEvictPod:
		p.Why = "Node pressure / eviction signal; reschedule pod onto a healthier node"
		p.ExpectedImpact = "Pod deleted and recreated by its controller"
		p.Rollback = "Controller recreates the pod; no further rollback"
		p.Risk = firstNonEmpty(p.Risk, "low")
	}
}

func riskFor(action string) string {
	switch action {
	case ActionEvictPod:
		return "low"
	case ActionRestartDeployment, ActionScaleDeployment, ActionRollbackFailedRollout:
		return "medium"
	default:
		return "high"
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
