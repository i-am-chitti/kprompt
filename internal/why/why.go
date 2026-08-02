// Package why builds structured causal chains for “why is X …?” prompts (S-003 · T-081).
//
// Output is an ADR-0014 Investigation whose Findings are ordered symptom → proximate → root.
// Not a chat scroll; optional suggested fixes stay PlanResult-shaped (approve required).
package why

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/kprompt/kprompt/internal/cluster"
	"github.com/kprompt/kprompt/internal/incident"
	"github.com/kprompt/kprompt/internal/pretrust"
)

// Request identifies a workload or pod to explain causally.
type Request struct {
	Name      string
	Namespace string
	Kind      string // Pod or Deployment
	Prompt    string
}

// Analyzer walks pod/deployment status into a causal Investigation.
type Analyzer struct {
	Client kubernetes.Interface
}

// Run returns an Investigation whose findings form a cause tree (ordered).
func (a *Analyzer) Run(ctx context.Context, req Request) (incident.Investigation, error) {
	if a == nil || a.Client == nil {
		return incident.Investigation{}, fmt.Errorf("why: client required")
	}
	ns := strings.TrimSpace(req.Namespace)
	if ns == "" {
		ns = "default"
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return incident.Investigation{}, fmt.Errorf("why: target name required")
	}
	kind := cluster.NormalizeKind(req.Kind)
	if kind != "Pod" && kind != "Deployment" {
		kind = "Deployment"
	}

	pod, targetKind, targetName, err := a.resolvePod(ctx, ns, name, kind)
	if err != nil {
		return incident.Investigation{}, err
	}

	out := incident.NewInvestigation(req.Prompt, ns)
	out.Target = &incident.ResourceRef{Kind: targetKind, Name: targetName, Namespace: ns}
	out.Degraded = []string{"mesh", "prometheus"}

	steps := a.causeTree(ctx, ns, pod)
	if len(steps) == 0 {
		steps = []incident.Finding{{
			Code:      "Healthy",
			Severity:  incident.SeverityInfo,
			Title:     "No causal problem signal",
			Message:   fmt.Sprintf("Pod/%s phase=%s looks fine to heuristics", pod.Name, pod.Status.Phase),
			Namespace: ns,
		}}
	}

	out.Findings = steps
	out.Evidence = evidenceFromPod(pod, ns)
	out.Timeline = append([]incident.EvidenceRef(nil), out.Evidence...)
	out.RootCause = steps[len(steps)-1].Message
	out.Confidence = confidenceFor(steps)
	out.Summary = summarizeTree(targetKind, targetName, pod, steps)
	out.SuggestedPlanHint = planHint(steps, targetName)

	pre := pretrust.Investigation(ctx, a.Client, out)
	pretrust.Apply(&out, pre)

	if err := incident.ValidateInvestigation(out); err != nil {
		return out, err
	}
	return out, nil
}

func (a *Analyzer) resolvePod(ctx context.Context, ns, name, kind string) (*corev1.Pod, string, string, error) {
	if kind == "Pod" {
		pod, err := a.Client.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, "", "", err
		}
		return pod, "Pod", name, nil
	}
	dep, err := a.Client.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		pod, perr := a.Client.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
		if perr != nil {
			return nil, "", "", err
		}
		return pod, "Pod", name, nil
	}
	if err != nil {
		return nil, "", "", err
	}
	pods, err := a.Client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
		LabelSelector: metav1.FormatLabelSelector(dep.Spec.Selector),
	})
	if err != nil {
		return nil, "", "", err
	}
	if len(pods.Items) == 0 {
		return nil, "", "", fmt.Errorf("why: Deployment/%s has no pods", name)
	}
	worst := &pods.Items[0]
	for i := range pods.Items {
		if problemScore(pods.Items[i]) > problemScore(*worst) {
			worst = &pods.Items[i]
		}
	}
	return worst, "Deployment", name, nil
}

func (a *Analyzer) causeTree(ctx context.Context, ns string, pod *corev1.Pod) []incident.Finding {
	var steps []incident.Finding

	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting != nil {
			reason := cs.State.Waiting.Reason
			msg := cs.State.Waiting.Message
			switch reason {
			case "CrashLoopBackOff":
				steps = append(steps, finding("Symptom.CrashLoop", incident.SeverityHigh,
					"Pod is crash-looping",
					fmt.Sprintf("container %s is in CrashLoopBackOff", cs.Name), ns))
				if cs.LastTerminationState.Terminated != nil {
					term := cs.LastTerminationState.Terminated
					if term.Reason == "OOMKilled" {
						steps = append(steps, finding("Cause.OOMKilled", incident.SeverityCritical,
							"Last exit was OOMKilled",
							fmt.Sprintf("container %s was OOMKilled (exit %d)", cs.Name, term.ExitCode), ns))
					} else {
						steps = append(steps, finding("Cause.ExitNonZero", incident.SeverityHigh,
							"Last exit was non-zero",
							fmt.Sprintf("container %s last exit code %d (%s)", cs.Name, term.ExitCode, firstNonEmpty(term.Reason, "Error")), ns))
					}
				}
				return steps
			case "ImagePullBackOff", "ErrImagePull":
				steps = append(steps,
					finding("Symptom.ImagePull", incident.SeverityHigh, "Image cannot be pulled",
						fmt.Sprintf("container %s: %s", cs.Name, firstNonEmpty(msg, reason)), ns),
					finding("Cause.BadImageRef", incident.SeverityHigh, "Image reference or credentials",
						"Verify image name/tag and imagePullSecrets", ns),
				)
				return steps
			}
		}
		if cs.LastTerminationState.Terminated != nil && cs.LastTerminationState.Terminated.Reason == "OOMKilled" {
			term := cs.LastTerminationState.Terminated
			steps = append(steps,
				finding("Symptom.OOM", incident.SeverityCritical, "Container was OOMKilled",
					fmt.Sprintf("container %s OOMKilled (exit %d)", cs.Name, term.ExitCode), ns),
				finding("Cause.MemoryLimit", incident.SeverityHigh, "Memory limit too low or leak",
					"Raise memory limit/request or fix the leak; check recent traffic/deploy", ns),
			)
			return steps
		}
	}

	if probeSteps := a.probeFailures(ctx, ns, pod); len(probeSteps) > 0 {
		return probeSteps
	}

	if pod.Status.Phase == corev1.PodPending || hasUnschedulable(pod) {
		steps = append(steps, finding("Symptom.Pending", incident.SeverityMedium,
			"Pod is Pending",
			fmt.Sprintf("Pod/%s phase=%s", pod.Name, pod.Status.Phase), ns))

		schedMsg := unschedulableMessage(pod)
		if schedMsg == "" {
			schedMsg = a.failedSchedulingMessage(ctx, ns, pod.Name)
		}
		if schedMsg != "" {
			steps = append(steps, finding("Cause.Unschedulable", incident.SeverityHigh,
				"Scheduler could not place the Pod",
				schedMsg, ns))
		}
		steps = append(steps, a.pendingRootCauses(ctx, ns, pod, schedMsg)...)
		return steps
	}

	return steps
}

// probeFailures returns Symptom.ProbeFail → Cause.{Readiness,Liveness}Probe when
// Unhealthy events or a not-ready container with a configured probe is present.
func (a *Analyzer) probeFailures(ctx context.Context, ns string, pod *corev1.Pod) []incident.Finding {
	if pod.Status.Phase == corev1.PodPending || hasUnschedulable(pod) {
		return nil
	}
	kind, msg, container := a.unhealthyProbeSignal(ctx, ns, pod.Name)
	if kind == "" {
		kind, msg, container = notReadyProbeSignal(pod)
	}
	if kind == "" {
		return nil
	}
	causeCode := "Cause.ReadinessProbe"
	causeTitle := "Readiness probe failing"
	if kind == "liveness" {
		causeCode = "Cause.LivenessProbe"
		causeTitle = "Liveness probe failing"
	}
	return []incident.Finding{
		finding("Symptom.ProbeFail", incident.SeverityHigh, "Container probe is failing",
			fmt.Sprintf("container %s: %s", firstNonEmpty(container, "unknown"), firstNonEmpty(msg, kind+" probe failing")), ns),
		finding(causeCode, incident.SeverityHigh, causeTitle,
			firstNonEmpty(msg, causeTitle+"; consider relaxing initialDelaySeconds / failureThreshold or fixing the app"), ns),
	}
}

func (a *Analyzer) unhealthyProbeSignal(ctx context.Context, ns, podName string) (kind, msg, container string) {
	list, err := a.Client.CoreV1().Events(ns).List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.kind=Pod,involvedObject.name=%s", podName),
	})
	if err != nil {
		return "", "", ""
	}
	for i := len(list.Items) - 1; i >= 0; i-- {
		ev := list.Items[i]
		if ev.Reason != "Unhealthy" {
			continue
		}
		lower := strings.ToLower(ev.Message)
		container = containerFromProbeMessage(ev.Message)
		switch {
		case strings.Contains(lower, "liveness"):
			return "liveness", strings.TrimSpace(ev.Message), container
		case strings.Contains(lower, "readiness"), strings.Contains(lower, "startup"):
			return "readiness", strings.TrimSpace(ev.Message), container
		default:
			return "readiness", strings.TrimSpace(ev.Message), container
		}
	}
	return "", "", ""
}

func notReadyProbeSignal(pod *corev1.Pod) (kind, msg, container string) {
	if pod.Status.Phase != corev1.PodRunning {
		return "", "", ""
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Ready {
			continue
		}
		spec := containerSpec(pod, cs.Name)
		if spec == nil {
			continue
		}
		switch {
		case spec.ReadinessProbe != nil:
			return "readiness",
				fmt.Sprintf("container %s is not Ready and has a readinessProbe", cs.Name),
				cs.Name
		case spec.LivenessProbe != nil:
			return "liveness",
				fmt.Sprintf("container %s is not Ready and has a livenessProbe", cs.Name),
				cs.Name
		}
	}
	return "", "", ""
}

func containerSpec(pod *corev1.Pod, name string) *corev1.Container {
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == name {
			return &pod.Spec.Containers[i]
		}
	}
	return nil
}

var probeContainerRE = regexp.MustCompile(`(?i)container\s+["']?([a-z0-9][-a-z0-9]*)`)

func containerFromProbeMessage(msg string) string {
	m := probeContainerRE.FindStringSubmatch(msg)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

func (a *Analyzer) pendingRootCauses(ctx context.Context, ns string, pod *corev1.Pod, schedMsg string) []incident.Finding {
	lower := strings.ToLower(schedMsg)
	var out []incident.Finding

	if strings.Contains(lower, "persistentvolumeclaim") || strings.Contains(lower, "unbound") || strings.Contains(lower, "pvc") {
		for _, v := range pod.Spec.Volumes {
			if v.PersistentVolumeClaim == nil {
				continue
			}
			claim := v.PersistentVolumeClaim.ClaimName
			pvc, err := a.Client.CoreV1().PersistentVolumeClaims(ns).Get(ctx, claim, metav1.GetOptions{})
			if err != nil {
				out = append(out, finding("Cause.PVCMissing", incident.SeverityHigh,
					"PVC not found",
					fmt.Sprintf("volume %s references PVC/%s: %v", v.Name, claim, err), ns))
				continue
			}
			out = append(out, finding("Cause.PVCPending", incident.SeverityHigh,
				"PVC is not Bound",
				fmt.Sprintf("PVC/%s phase=%s storageClass=%s", claim, pvc.Status.Phase, pvcClass(pvc)), ns))
			if pvc.Status.Phase == corev1.ClaimPending {
				scName := pvcClass(pvc)
				if scName != "" {
					_, err := a.Client.StorageV1().StorageClasses().Get(ctx, scName, metav1.GetOptions{})
					if apierrors.IsNotFound(err) {
						out = append(out, finding("Cause.MissingStorageClass", incident.SeverityCritical,
							"StorageClass does not exist",
							fmt.Sprintf("StorageClass %q is missing — PVC/%s cannot bind", scName, claim), ns))
					} else if err != nil {
						out = append(out, finding("Cause.StorageClassError", incident.SeverityMedium,
							"Could not read StorageClass", err.Error(), ns))
					}
				}
			}
		}
		return out
	}

	if strings.Contains(lower, "affinity") || strings.Contains(lower, "match node selectors") || strings.Contains(lower, "node selector") {
		out = append(out, finding("Cause.NodeSelector", incident.SeverityHigh,
			"No nodes match selector/affinity",
			firstNonEmpty(schedMsg, "node affinity / selector unmatched"), ns))
		if strings.Contains(lower, "gpu") || hasGPURequest(pod) {
			nodes, err := a.Client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
			if err == nil {
				gpuNodes := 0
				for _, n := range nodes.Items {
					if nodeHasGPU(n) {
						gpuNodes++
					}
				}
				if gpuNodes == 0 {
					out = append(out, finding("Cause.NoGPUNodes", incident.SeverityCritical,
						"Cluster has zero GPU nodes",
						fmt.Sprintf("0/%d nodes expose a GPU capacity; affinity/request cannot be satisfied", len(nodes.Items)), ns))
				}
			}
		}
		return out
	}

	if strings.Contains(lower, "taint") || strings.Contains(lower, "toleration") {
		out = append(out, finding("Cause.TaintToleration", incident.SeverityHigh,
			"Taints block scheduling",
			firstNonEmpty(schedMsg, "pod does not tolerate node taints"), ns))
		return out
	}

	if strings.Contains(lower, "insufficient") || strings.Contains(lower, "too many pods") {
		out = append(out, finding("Cause.ResourcePressure", incident.SeverityHigh,
			"Cluster capacity insufficient",
			firstNonEmpty(schedMsg, "not enough CPU/memory/pod slots"), ns))
		return out
	}

	if schedMsg != "" {
		out = append(out, finding("Cause.UnknownSchedule", incident.SeverityMedium,
			"Unclassified scheduling failure", schedMsg, ns))
	}
	return out
}

func (a *Analyzer) failedSchedulingMessage(ctx context.Context, ns, podName string) string {
	list, err := a.Client.CoreV1().Events(ns).List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.kind=Pod,involvedObject.name=%s", podName),
	})
	if err != nil {
		return ""
	}
	for i := len(list.Items) - 1; i >= 0; i-- {
		ev := list.Items[i]
		if ev.Reason == "FailedScheduling" {
			return strings.TrimSpace(ev.Message)
		}
	}
	return ""
}

func evidenceFromPod(pod *corev1.Pod, ns string) []incident.EvidenceRef {
	var out []incident.EvidenceRef
	out = append(out, incident.EvidenceRef{
		Type: incident.EvidenceObject,
		Resource: &incident.ResourceRef{
			Kind: "Pod", Name: pod.Name, Namespace: ns,
		},
		Reason:  string(pod.Status.Phase),
		Message: fmt.Sprintf("phase=%s", pod.Status.Phase),
		Source:  "kubernetes",
	})
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse {
			out = append(out, incident.EvidenceRef{
				Type:    incident.EvidenceEvent,
				Reason:  string(cond.Reason),
				Message: cond.Message,
				Source:  "kubernetes",
				Resource: &incident.ResourceRef{
					Kind: "Pod", Name: pod.Name, Namespace: ns,
				},
			})
		}
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting != nil {
			out = append(out, incident.EvidenceRef{
				Type:    incident.EvidenceObject,
				Reason:  cs.State.Waiting.Reason,
				Message: cs.State.Waiting.Message,
				Source:  "kubernetes",
				Resource: &incident.ResourceRef{
					Kind: "Pod", Name: pod.Name, Namespace: ns,
				},
			})
		}
	}
	return out
}

func finding(code, sev, title, msg, ns string) incident.Finding {
	return incident.Finding{
		Code: code, Severity: sev, Title: title, Message: msg, Namespace: ns,
	}
}

func summarizeTree(kind, name string, pod *corev1.Pod, steps []incident.Finding) string {
	if len(steps) == 0 {
		return fmt.Sprintf("%s/%s: no causal findings (Pod/%s phase=%s)", kind, name, pod.Name, pod.Status.Phase)
	}
	var chain []string
	for _, s := range steps {
		chain = append(chain, s.Title)
	}
	return fmt.Sprintf("%s/%s: %s", kind, name, strings.Join(chain, " → "))
}

func confidenceFor(steps []incident.Finding) float64 {
	if len(steps) == 0 {
		return 0.4
	}
	last := steps[len(steps)-1].Code
	switch {
	case strings.HasPrefix(last, "Cause.MissingStorageClass"),
		strings.HasPrefix(last, "Cause.NoGPUNodes"),
		strings.HasPrefix(last, "Cause.OOMKilled"),
		strings.HasPrefix(last, "Cause.BadImageRef"),
		strings.HasPrefix(last, "Cause.ReadinessProbe"),
		strings.HasPrefix(last, "Cause.LivenessProbe"):
		return 0.9
	case strings.HasPrefix(last, "Cause."):
		return 0.8
	case strings.HasPrefix(last, "Symptom."):
		return 0.6
	default:
		return 0.5
	}
}

func planHint(steps []incident.Finding, target string) string {
	for i := len(steps) - 1; i >= 0; i-- {
		switch steps[i].Code {
		case "Cause.MissingStorageClass":
			return "Fix: create the StorageClass or point the PVC at an existing one, then delete the Pending Pod"
		case "Cause.MemoryLimit", "Cause.OOMKilled":
			return fmt.Sprintf("Suggested (approve required): raise memory for %s", target)
		case "Cause.BadImageRef":
			return fmt.Sprintf("Suggested: name a replacement — set %s image to <tag> — for a reviewable plan", target)
		case "Cause.ExitNonZero":
			return fmt.Sprintf("Suggested (approve required): rollback %s when a prior revision exists; else kprompt \"logs %s\"", target, target)
		case "Cause.ReadinessProbe", "Cause.LivenessProbe":
			return fmt.Sprintf("Suggested (approve required): relax probe timing on %s, or fix the app health endpoint", target)
		case "Cause.NoGPUNodes":
			return "Fix: add GPU nodes or remove the GPU request/affinity"
		case "Cause.PVCMissing", "Cause.PVCPending":
			return fmt.Sprintf("Next: describe PVC / StorageClass for %s — no auto-delete of claims", target)
		case "Cause.NodeSelector", "Cause.TaintToleration", "Cause.ResourcePressure", "Cause.UnknownSchedule":
			return fmt.Sprintf("Next: kprompt \"describe %s\" — scheduling fixes are cluster/ops changes, not invented patches", target)
		}
	}
	return ""
}

func hasUnschedulable(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse {
			return true
		}
	}
	return false
}

func unschedulableMessage(pod *corev1.Pod) string {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse {
			return strings.TrimSpace(cond.Message)
		}
	}
	return ""
}

func pvcClass(pvc *corev1.PersistentVolumeClaim) string {
	if pvc.Spec.StorageClassName != nil {
		return *pvc.Spec.StorageClassName
	}
	if v, ok := pvc.Annotations["volume.beta.kubernetes.io/storage-class"]; ok {
		return v
	}
	return ""
}

func hasGPURequest(pod *corev1.Pod) bool {
	for _, c := range pod.Spec.Containers {
		if _, ok := c.Resources.Requests["nvidia.com/gpu"]; ok {
			return true
		}
		if _, ok := c.Resources.Limits["nvidia.com/gpu"]; ok {
			return true
		}
	}
	return false
}

func nodeHasGPU(n corev1.Node) bool {
	if _, ok := n.Status.Allocatable["nvidia.com/gpu"]; ok {
		return true
	}
	if _, ok := n.Status.Capacity["nvidia.com/gpu"]; ok {
		return true
	}
	return false
}

func problemScore(p corev1.Pod) int {
	score := 0
	switch p.Status.Phase {
	case corev1.PodFailed:
		score += 100
	case corev1.PodPending:
		score += 50
	case corev1.PodRunning:
		score += 10
	}
	for _, cs := range p.Status.ContainerStatuses {
		score += int(cs.RestartCount)
		if !cs.Ready {
			score += 5
		}
		if cs.State.Waiting != nil {
			switch cs.State.Waiting.Reason {
			case "CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull":
				score += 40
			}
		}
		if !cs.Ready && p.Status.Phase == corev1.PodRunning {
			score += 15
		}
	}
	if hasUnschedulable(&p) {
		score += 60
	}
	return score
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
