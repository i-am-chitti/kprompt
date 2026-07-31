// Package detect is the Namespace Agent anomaly detector catalog (AG-026).
//
// Detectors emit evidence-backed hits with optional causal-chain hints.
// They never invent cluster state and never mutate (ADR-0016).
package detect

import (
	"strings"

	"github.com/kprompt/kprompt/internal/agent/ctxbuild"
	"github.com/kprompt/kprompt/internal/incident"
)

// Hit is one detector match against an AgentContext blob.
type Hit struct {
	Code        string   // e.g. oom.killed, image.pull, crashloop
	Severity    string
	Confidence  float64
	Summary     string
	RootCause   string // probable root (not merely the symptom label)
	CausalChain []string
	Alternatives []string
	Recommendation string
}

// Catalog runs heuristic detectors in priority order; first match wins.
// Priority mirrors ADR-0016 §5 where practical (outage/availability before soft signals).
func Catalog(agentCtx ctxbuild.AgentContext) (Hit, bool) {
	blob := strings.ToLower(buildBlob(agentCtx))
	for _, d := range detectors {
		if hit, ok := d(blob, agentCtx); ok {
			return hit, true
		}
	}
	return Hit{}, false
}

type detectorFunc func(blob string, ctx ctxbuild.AgentContext) (Hit, bool)

var detectors = []detectorFunc{
	detectOOM,
	detectImagePull,
	detectCrashLoop,
	detectFailedScheduling,
	detectQuota,
	detectHPA,
	detectProbe,
	detectRollout,
	detectDNS,
	detectStorage,
}

func detectOOM(blob string, _ ctxbuild.AgentContext) (Hit, bool) {
	if !strings.Contains(blob, "oom") {
		return Hit{}, false
	}
	return Hit{
		Code:       "oom.killed",
		Severity:   incident.SeverityCritical,
		Confidence: 0.85,
		Summary:    "Workload hit memory limit (OOMKilled)",
		RootCause:  "Memory exhaustion (limit exceeded or leak)",
		CausalChain: []string{
			"High latency or restarts",
			"Pod restart",
			"OOMKilled",
			"Memory exhaustion",
		},
		Alternatives:   []string{"Node memory pressure", "Memory fragmentation", "Traffic spike"},
		Recommendation: "Raise memory limit/request or fix memory leak; check recent traffic/deploy",
	}, true
}

func detectImagePull(blob string, _ ctxbuild.AgentContext) (Hit, bool) {
	if !(strings.Contains(blob, "imagepull") ||
		strings.Contains(blob, "errimage") ||
		strings.Contains(blob, "failed to pull") ||
		strings.Contains(blob, "pulling image") ||
		strings.Contains(blob, "manifest unknown")) {
		return Hit{}, false
	}
	return Hit{
		Code:       "image.pull",
		Severity:   incident.SeverityHigh,
		Confidence: 0.85,
		Summary:    "Image pull failure",
		RootCause:  "Image name/tag or registry credentials invalid",
		CausalChain: []string{
			"Pod Pending or CrashLoop",
			"ImagePullBackOff / ErrImagePull",
			"Registry pull failed",
		},
		Alternatives:   []string{"Network policy blocking registry", "Node disk pressure"},
		Recommendation: "Verify image name/tag and registry credentials (imagePullSecrets)",
	}, true
}

func detectCrashLoop(blob string, _ ctxbuild.AgentContext) (Hit, bool) {
	if !(strings.Contains(blob, "crashloop") || strings.Contains(blob, "backoff")) {
		return Hit{}, false
	}
	// Avoid stealing ImagePull thin BackOff — image.pull runs first.
	return Hit{
		Code:       "crashloop",
		Severity:   incident.SeverityHigh,
		Confidence: 0.8,
		Summary:    "Container repeatedly crashing (CrashLoopBackOff)",
		RootCause:  "Process exit / probe failure / bad config",
		CausalChain: []string{
			"Pod not Ready",
			"Container restart loop",
			"CrashLoopBackOff",
			"Application or config fault",
		},
		Alternatives:   []string{"Missing Secret/ConfigMap", "Bad probe", "Dependency down"},
		Recommendation: "Check previous container logs and readiness/liveness probes; verify config/secret refs",
	}, true
}

func detectFailedScheduling(blob string, _ ctxbuild.AgentContext) (Hit, bool) {
	if !(strings.Contains(blob, "failedscheduling") || strings.Contains(blob, "pending")) {
		return Hit{}, false
	}
	return Hit{
		Code:       "schedule.pending",
		Severity:   incident.SeverityMedium,
		Confidence: 0.75,
		Summary:    "Pod cannot be scheduled",
		RootCause:  "Scheduler cannot place pod (resources, taints, or PVC)",
		CausalChain: []string{
			"Pod Pending",
			"FailedScheduling or unbound volume",
			"Insufficient node capacity / taints / PVC",
		},
		Alternatives:   []string{"ResourceQuota denial", "Affinity conflict"},
		Recommendation: "Check node resources, taints/tolerations, and PVC binding",
	}, true
}

func detectProbe(blob string, _ ctxbuild.AgentContext) (Hit, bool) {
	if !(strings.Contains(blob, "unhealthy") || strings.Contains(blob, "probe") || strings.Contains(blob, "liveness") || strings.Contains(blob, "readiness")) {
		return Hit{}, false
	}
	return Hit{
		Code:       "probe.fail",
		Severity:   incident.SeverityMedium,
		Confidence: 0.7,
		Summary:    "Probe failing / container marked unhealthy",
		RootCause:  "Readiness/liveness probe failing",
		CausalChain: []string{
			"Endpoints empty or restarts",
			"Probe failure",
			"Application not ready or probe misconfigured",
		},
		Alternatives:   []string{"Slow startup", "Dependency timeout"},
		Recommendation: "Review probe paths/timeouts and application readiness",
	}, true
}

func detectRollout(blob string, ctx ctxbuild.AgentContext) (Hit, bool) {
	if ctx.Deployment != nil && strings.TrimSpace(ctx.Deployment.ChangeCause) != "" {
		// Soft signal — only when no harder detector matched (catalog order).
		// This detector runs after crash/oom; use as enrichment via CatalogEnrich instead.
	}
	if !(strings.Contains(blob, "progressdeadline") ||
		strings.Contains(blob, "progressing=false") ||
		(strings.Contains(blob, "rollout") && strings.Contains(blob, "failed"))) {
		return Hit{}, false
	}
	return Hit{
		Code:       "rollout.failed",
		Severity:   incident.SeverityHigh,
		Confidence: 0.8,
		Summary:    "Deployment rollout failing",
		RootCause:  "New ReplicaSet not becoming available",
		CausalChain: []string{
			"Deployment Progressing=False",
			"New pods not Ready",
			"Rollout blocked",
		},
		Alternatives:   []string{"Image pull failure", "Probe failure", "Config regression"},
		Recommendation: "Inspect new ReplicaSet pods; consider rollback if prior revision was healthy",
	}, true
}

func detectQuota(blob string, _ ctxbuild.AgentContext) (Hit, bool) {
	if !(strings.Contains(blob, "exceeded quota") ||
		strings.Contains(blob, "forbidden: exceeded quota") ||
		strings.Contains(blob, "resourcequota") ||
		strings.Contains(blob, "limited by") && strings.Contains(blob, "quota")) {
		return Hit{}, false
	}
	return Hit{
		Code:       "quota.exceeded",
		Severity:   incident.SeverityHigh,
		Confidence: 0.82,
		Summary:    "ResourceQuota denial / exceeded",
		RootCause:  "Namespace ResourceQuota blocks create or scale",
		CausalChain: []string{
			"Pod Pending or create rejected",
			"Forbidden / exceeded quota",
			"ResourceQuota hard limit",
		},
		Alternatives:   []string{"LimitRange", "Cluster capacity"},
		Recommendation: "Inspect ResourceQuota used vs hard; free unused workloads or raise quota",
	}, true
}

func detectHPA(blob string, _ ctxbuild.AgentContext) (Hit, bool) {
	if !(strings.Contains(blob, "horizontalpodautoscaler") ||
		strings.Contains(blob, "failedgetresource") ||
		strings.Contains(blob, "failedgetexternalmetric") ||
		strings.Contains(blob, "unable to get metrics") ||
		(strings.Contains(blob, "hpa") && (strings.Contains(blob, "failed") || strings.Contains(blob, "metric")))) {
		return Hit{}, false
	}
	return Hit{
		Code:       "hpa.metric",
		Severity:   incident.SeverityMedium,
		Confidence: 0.72,
		Summary:    "HPA cannot scale from metrics",
		RootCause:  "Metrics API / custom metric unavailable for HorizontalPodAutoscaler",
		CausalChain: []string{
			"Replica count stuck",
			"HPA FailedGetResourceMetric / missing metrics",
			"Metrics-server or adapter gap",
		},
		Alternatives:   []string{"Target already at maxReplicas", "Wrong metric name"},
		Recommendation: "Check metrics-server / adapter and HPA status conditions; verify metric selectors",
	}, true
}

func detectDNS(blob string, _ ctxbuild.AgentContext) (Hit, bool) {
	if !(strings.Contains(blob, "no such host") ||
		strings.Contains(blob, "lookup ") && strings.Contains(blob, "failed") ||
		strings.Contains(blob, "getaddrinfo") ||
		strings.Contains(blob, "i/o timeout") && strings.Contains(blob, "dns")) {
		return Hit{}, false
	}
	return Hit{
		Code:       "dns.fail",
		Severity:   incident.SeverityHigh,
		Confidence: 0.78,
		Summary:    "DNS resolution failure",
		RootCause:  "Service DNS name unresolved or CoreDNS issue",
		CausalChain: []string{
			"Connection errors in logs",
			"DNS lookup failure",
			"Missing Service/Endpoints or CoreDNS fault",
		},
		Alternatives:   []string{"Wrong Service name", "NetworkPolicy"},
		Recommendation: "Verify Service/Endpoints and CoreDNS; check dependent service name",
	}, true
}

func detectStorage(blob string, _ ctxbuild.AgentContext) (Hit, bool) {
	if !(strings.Contains(blob, "failedattachvolume") ||
		strings.Contains(blob, "failedmount") ||
		strings.Contains(blob, "unbound") && strings.Contains(blob, "pvc") ||
		strings.Contains(blob, "persistentvolumeclaim")) {
		return Hit{}, false
	}
	return Hit{
		Code:       "storage.pvc",
		Severity:   incident.SeverityHigh,
		Confidence: 0.8,
		Summary:    "Volume / PVC binding or mount failure",
		RootCause:  "PVC unbound or volume attach/mount failed",
		CausalChain: []string{
			"Pod Pending",
			"PVC unbound or FailedMount",
			"Storage class / provisioner / attach fault",
		},
		Alternatives:   []string{"Quota", "Node selector mismatch"},
		Recommendation: "Check PVC status, StorageClass, and volume attach events",
	}, true
}

func buildBlob(agentCtx ctxbuild.AgentContext) string {
	inc := agentCtx.Incident
	summary := inc.Summary
	parts := []string{
		summary,
		joinEvidence(inc.Evidence),
		joinEvidence(agentCtx.LogSnippets),
		joinEvidence(agentCtx.RecentEvents),
		podSignals(agentCtx.Pod),
		deploymentSignals(agentCtx.Deployment),
	}
	return strings.Join(parts, " ")
}

func joinEvidence(ev []incident.EvidenceRef) string {
	var b strings.Builder
	for _, e := range ev {
		b.WriteString(e.Reason)
		b.WriteByte(' ')
		b.WriteString(e.Message)
		b.WriteByte(' ')
	}
	return b.String()
}

func podSignals(pod *ctxbuild.PodSnapshot) string {
	if pod == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(pod.Phase)
	b.WriteByte(' ')
	for _, c := range pod.Containers {
		b.WriteString(c.State)
		b.WriteByte(' ')
		b.WriteString(c.LastTermination)
		b.WriteByte(' ')
		b.WriteString(c.Image)
		b.WriteByte(' ')
	}
	return b.String()
}

func deploymentSignals(dep *ctxbuild.DeploymentSnapshot) string {
	if dep == nil {
		return ""
	}
	return dep.ChangeCause
}
