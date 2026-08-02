// Package pretrust implements independent verify before high confidence / approve UX (S-018 · T-089).
//
// No LLM. Callers pass only an Investigation (+ optional ExecutionPlan) and a kube client —
// never chat/session history. Complements post-apply verify (T-070).
package pretrust

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/kprompt/kprompt/internal/incident"
	"github.com/kprompt/kprompt/internal/planner"
	"github.com/kprompt/kprompt/internal/safety"
)

const (
	// HighConfidenceFloor is the bar that requires independent anchors (AG-068 twin).
	HighConfidenceFloor = 0.7
	// SoftAgreeConfidenceCap applies when high confidence lacks EvidenceRef or re-read fails.
	SoftAgreeConfidenceCap = 0.4
	// SourceReRead stamps fresh probe Evidence produced by this package.
	SourceReRead = "pretrust-reread"
)

// Check is one deterministic pre-trust assertion.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// Report is the outcome of independent verify (never raises confidence).
type Report struct {
	OK            bool    `json:"ok"`
	ConfidenceCap float64 `json:"confidenceCap,omitempty"` // if >0, clamp Investigation.Confidence to this
	Notes         []string
	Checks        []Check
	Denied        bool
	DenyMessage   string
}

// Investigation runs schema + EvidenceRef + optional target re-read checks.
func Investigation(ctx context.Context, client kubernetes.Interface, inv incident.Investigation) Report {
	rep := Report{OK: true}
	if err := incident.ValidateInvestigation(inv); err != nil {
		rep.OK = false
		rep.ConfidenceCap = SoftAgreeConfidenceCap
		rep.Checks = append(rep.Checks, Check{Name: "schema", OK: false, Detail: err.Error()})
		rep.Notes = append(rep.Notes, "pretrust: Investigation schema invalid — do not treat confidence as proof")
		return rep
	}
	rep.Checks = append(rep.Checks, Check{Name: "schema", OK: true})

	if inv.Confidence >= HighConfidenceFloor && !hasUsableEvidence(inv) {
		rep.OK = false
		rep.ConfidenceCap = SoftAgreeConfidenceCap
		rep.Checks = append(rep.Checks, Check{Name: "evidence", OK: false, Detail: "high confidence without EvidenceRef"})
		rep.Notes = append(rep.Notes, "pretrust: independent verify failed — high confidence without EvidenceRef (narrative soft-agree is not verification)")
	} else {
		rep.Checks = append(rep.Checks, Check{Name: "evidence", OK: true})
	}

	if client != nil && inv.Confidence >= HighConfidenceFloor && claimsProblem(inv) {
		ok, detail := rereadConfirms(ctx, client, inv)
		rep.Checks = append(rep.Checks, Check{Name: "reread", OK: ok, Detail: detail})
		if !ok {
			rep.OK = false
			rep.ConfidenceCap = SoftAgreeConfidenceCap
			rep.Notes = append(rep.Notes, "pretrust: re-read does not confirm claimed problem — "+detail)
		}
	}

	return rep
}

// SuggestedPlan checks a draft fix plan independently of the Investigation narrative.
func SuggestedPlan(_ context.Context, _ kubernetes.Interface, inv incident.Investigation, plan planner.ExecutionPlan) Report {
	rep := Report{OK: true}
	risk := safety.EvaluatePlan(plan)
	if risk.Denied {
		rep.OK = false
		rep.Denied = true
		rep.DenyMessage = risk.Message
		rep.Checks = append(rep.Checks, Check{Name: "hard-deny", OK: false, Detail: risk.Message})
		rep.Notes = append(rep.Notes, "pretrust: suggested plan hard-denied — approve UX skipped")
		return rep
	}
	rep.Checks = append(rep.Checks, Check{Name: "hard-deny", OK: true})

	if inv.Confidence >= HighConfidenceFloor && !hasUsableEvidence(inv) {
		rep.OK = false
		rep.ConfidenceCap = SoftAgreeConfidenceCap
		rep.Checks = append(rep.Checks, Check{Name: "evidence-gate", OK: false, Detail: "refuse approve UX for high-conf Investigation without EvidenceRef"})
		rep.Notes = append(rep.Notes, "pretrust: skipping approve UX — Investigation failed independent verify")
		return rep
	}
	rep.Checks = append(rep.Checks, Check{Name: "evidence-gate", OK: true})
	return rep
}

// Apply clamps Investigation confidence and stamps Degraded notes. Never raises confidence.
func Apply(inv *incident.Investigation, rep Report) {
	if inv == nil {
		return
	}
	if rep.ConfidenceCap > 0 && inv.Confidence > rep.ConfidenceCap {
		inv.Confidence = rep.ConfidenceCap
	}
	for _, n := range rep.Notes {
		inv.Degraded = appendUnique(inv.Degraded, n)
	}
	if !rep.OK && inv.SuggestedPlanHint != "" && rep.ConfidenceCap == SoftAgreeConfidenceCap {
		inv.SuggestedPlanHint = strings.TrimSpace(inv.SuggestedPlanHint + " (pretrust: verify anchors before approve)")
	}
}

func hasUsableEvidence(inv incident.Investigation) bool {
	for _, e := range inv.Evidence {
		if usableEvidence(e) {
			return true
		}
	}
	for _, f := range inv.Findings {
		for _, e := range f.Evidence {
			if usableEvidence(e) {
				return true
			}
		}
	}
	return false
}

func usableEvidence(e incident.EvidenceRef) bool {
	switch strings.ToLower(strings.TrimSpace(e.Type)) {
	case incident.EvidenceEvent, incident.EvidenceLog, incident.EvidenceObject,
		incident.EvidenceMetric, incident.EvidenceTrace, incident.EvidenceGitOps:
		if e.Resource != nil && e.Resource.Name != "" {
			return true
		}
		if strings.TrimSpace(e.Reason) != "" || strings.TrimSpace(e.Message) != "" || strings.TrimSpace(e.Source) != "" {
			return true
		}
	}
	return false
}

func claimsProblem(inv incident.Investigation) bool {
	blob := strings.ToLower(inv.RootCause + " " + inv.Summary + " " + inv.SuggestedPlanHint)
	for _, f := range inv.Findings {
		blob += " " + strings.ToLower(f.Code+" "+f.Title+" "+f.Message)
	}
	needles := []string{
		"crashloop", "oomkill", "imagepull", "errimage", "pending",
		"unhealthy", "probe", "backoff", "no ready", "not ready",
	}
	for _, n := range needles {
		if strings.Contains(blob, n) {
			return true
		}
	}
	return false
}

func rereadConfirms(ctx context.Context, client kubernetes.Interface, inv incident.Investigation) (bool, string) {
	if inv.Target == nil || strings.TrimSpace(inv.Target.Name) == "" {
		return true, "no target to re-read"
	}
	ns := strings.TrimSpace(inv.Target.Namespace)
	if ns == "" {
		ns = strings.TrimSpace(inv.Namespace)
	}
	if ns == "" {
		ns = "default"
	}
	name := inv.Target.Name
	kind := strings.ToLower(inv.Target.Kind)

	var pods []corev1.Pod
	switch kind {
	case "pod":
		pod, err := client.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return true, "target pod gone (problem may have resolved or moved)"
		}
		if err != nil {
			return true, fmt.Sprintf("re-read skipped: %v", err)
		}
		pods = []corev1.Pod{*pod}
	case "deployment", "":
		dep, err := client.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return true, "target deployment gone"
		}
		if err != nil {
			return true, fmt.Sprintf("re-read skipped: %v", err)
		}
		list, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
			LabelSelector: metav1.FormatLabelSelector(dep.Spec.Selector),
		})
		if err != nil {
			return true, fmt.Sprintf("re-read skipped: %v", err)
		}
		pods = list.Items
	default:
		return true, "re-read not applicable for kind " + inv.Target.Kind
	}

	if len(pods) == 0 {
		if claimsReadyEndpointsIssue(inv) {
			return true, "no pods — consistent with readiness/endpoints issue"
		}
		return false, "re-read found zero pods while Investigation claimed a live workload problem"
	}

	if anyProblemPod(pods) {
		return true, "re-read still sees problem signals on pods"
	}
	if claimsCrashOrPullOrOOM(inv) {
		return false, "re-read pods look ready; claimed CrashLoop/OOM/ImagePull not confirmed"
	}
	return true, "re-read did not contradict findings"
}

func claimsCrashOrPullOrOOM(inv incident.Investigation) bool {
	blob := strings.ToLower(inv.RootCause + " " + inv.Summary)
	for _, f := range inv.Findings {
		blob += " " + strings.ToLower(f.Code+" "+f.Message)
	}
	for _, n := range []string{"crashloop", "oomkill", "imagepull", "errimagepull", "errimage"} {
		if strings.Contains(blob, n) {
			return true
		}
	}
	return false
}

func claimsReadyEndpointsIssue(inv incident.Investigation) bool {
	blob := strings.ToLower(inv.RootCause + " " + inv.Summary)
	for _, f := range inv.Findings {
		blob += " " + strings.ToLower(f.Code)
	}
	return strings.Contains(blob, "endpoint") || strings.Contains(blob, "not ready") || strings.Contains(blob, "noready")
}

func anyProblemPod(pods []corev1.Pod) bool {
	for _, p := range pods {
		if p.Status.Phase == corev1.PodPending || p.Status.Phase == corev1.PodFailed {
			return true
		}
		for _, cs := range p.Status.ContainerStatuses {
			if cs.State.Waiting != nil {
				r := cs.State.Waiting.Reason
				if r == "CrashLoopBackOff" || r == "ImagePullBackOff" || r == "ErrImagePull" || r == "CreateContainerConfigError" {
					return true
				}
			}
			if cs.LastTerminationState.Terminated != nil && cs.LastTerminationState.Terminated.Reason == "OOMKilled" {
				return true
			}
			if !cs.Ready && p.Status.Phase == corev1.PodRunning {
				return true
			}
		}
	}
	return false
}

func appendUnique(in []string, s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return in
	}
	for _, x := range in {
		if x == s {
			return in
		}
	}
	return append(in, s)
}
