// Package investigate builds multi-hop RCA documents (S-002 · T-080).
//
// MVP hops: Service → Endpoints → Deployment → ReplicaSet → Pods → Events → Logs.
// Ingress / mesh / Prometheus are listed in Investigation.Degraded until later slices.
package investigate

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"

	"github.com/kprompt/kprompt/internal/cluster"
	"github.com/kprompt/kprompt/internal/incident"
)

// Request identifies a workload to investigate.
type Request struct {
	Name      string
	Namespace string
	Kind      string // Pod or Deployment
	Prompt    string
}

// Investigator walks related objects and emits an Investigation.
type Investigator struct {
	Client kubernetes.Interface
}

// Run performs the multi-hop walk and returns a validated Investigation.
func (inv *Investigator) Run(ctx context.Context, req Request) (incident.Investigation, cluster.ExplainReport, error) {
	if inv == nil || inv.Client == nil {
		return incident.Investigation{}, cluster.ExplainReport{}, fmt.Errorf("investigate: client required")
	}
	ns := strings.TrimSpace(req.Namespace)
	if ns == "" {
		ns = "default"
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return incident.Investigation{}, cluster.ExplainReport{}, fmt.Errorf("investigate: target name required")
	}
	kind := cluster.NormalizeKind(req.Kind)
	if kind != "Pod" && kind != "Deployment" {
		kind = "Deployment"
	}

	explainer := &cluster.Explainer{Client: inv.Client}
	rep, err := explainer.Explain(ctx, cluster.ExplainRequest{
		Name: name, Namespace: ns, Kind: kind,
	})
	if err != nil {
		return incident.Investigation{}, rep, err
	}

	out := incident.NewInvestigation(req.Prompt, ns)
	out.Target = &incident.ResourceRef{Kind: rep.Kind, Name: rep.Target, Namespace: ns}
	out.Summary = firstNonEmpty(rep.Summary, fmt.Sprintf("%s/%s status: %s", rep.Kind, rep.Target, rep.Status))
	out.Degraded = []string{"ingress", "mesh", "prometheus"} // honest MVP gaps

	// Service → Endpoints ahead of the Deployment chain when we can resolve them.
	svcHops, svcFindings, svcEvidence := inv.serviceHops(ctx, ns, rep)
	prependChain(&rep, svcHops...)

	out.Findings = append(out.Findings, svcFindings...)
	out.Findings = append(out.Findings, mapExplainFindings(rep)...)
	out.Evidence = append(out.Evidence, svcEvidence...)
	out.Evidence = append(out.Evidence, mapExplainEvidence(rep)...)
	out.Timeline = append([]incident.EvidenceRef(nil), out.Evidence...)

	out.RootCause, out.Confidence = rootCauseFrom(rep, out.Findings)
	out.SuggestedPlanHint = planHintFrom(rep)

	if err := incident.ValidateInvestigation(out); err != nil {
		return out, rep, err
	}
	return out, rep, nil
}

func (inv *Investigator) serviceHops(ctx context.Context, ns string, rep cluster.ExplainReport) (
	hops []cluster.ChainStep,
	findings []incident.Finding,
	evidence []incident.EvidenceRef,
) {
	podLabels, err := inv.workloadSelector(ctx, ns, rep)
	if err != nil || len(podLabels) == 0 {
		return nil, nil, nil
	}

	svcs, err := inv.Client.CoreV1().Services(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		findings = append(findings, incident.Finding{
			Code:     "ServiceListError",
			Severity: incident.SeverityLow,
			Title:    "Could not list Services",
			Message:  err.Error(),
		})
		return hops, findings, evidence
	}

	for _, svc := range svcs.Items {
		if svc.Spec.Selector == nil || len(svc.Spec.Selector) == 0 {
			continue
		}
		if !selectorMatches(svc.Spec.Selector, podLabels) {
			continue
		}
		ports := formatPorts(svc.Spec.Ports)
		hops = append(hops, cluster.ChainStep{
			Level:  "Service",
			Name:   svc.Name,
			Detail: firstNonEmpty(ports, "selector matches workload"),
		})
		evidence = append(evidence, incident.EvidenceRef{
			Type: incident.EvidenceObject,
			Resource: &incident.ResourceRef{
				Kind: "Service", Name: svc.Name, Namespace: ns,
			},
			Reason:  "Selected",
			Message: "Service selector matches workload pod labels",
			Source:  "kubernetes",
		})

		ep, err := inv.Client.CoreV1().Endpoints(ns).Get(ctx, svc.Name, metav1.GetOptions{})
		if err != nil {
			hops = append(hops, cluster.ChainStep{
				Level:  "Endpoints",
				Name:   svc.Name,
				Detail: "unavailable: " + err.Error(),
			})
			findings = append(findings, incident.Finding{
				Code:      "EndpointsUnavailable",
				Severity:  incident.SeverityMedium,
				Title:     "Endpoints missing for Service/" + svc.Name,
				Message:   err.Error(),
				Namespace: ns,
			})
			continue
		}
		ready, notReady := countAddresses(ep)
		detail := fmt.Sprintf("ready=%d notReady=%d", ready, notReady)
		hops = append(hops, cluster.ChainStep{
			Level:  "Endpoints",
			Name:   svc.Name,
			Detail: detail,
		})
		evidence = append(evidence, incident.EvidenceRef{
			Type: incident.EvidenceObject,
			Resource: &incident.ResourceRef{
				Kind: "Endpoints", Name: svc.Name, Namespace: ns,
			},
			Reason:  "Addresses",
			Message: detail,
			Source:  "kubernetes",
		})
		if ready == 0 {
			sev := incident.SeverityHigh
			findings = append(findings, incident.Finding{
				Code:      "NoReadyEndpoints",
				Severity:  sev,
				Title:     "Service/" + svc.Name + " has no ready endpoints",
				Message:   "No ready backend addresses — traffic to this Service will fail",
				Namespace: ns,
			})
		}
	}
	return hops, findings, evidence
}

func (inv *Investigator) workloadSelector(ctx context.Context, ns string, rep cluster.ExplainReport) (map[string]string, error) {
	switch rep.Kind {
	case "Deployment":
		dep, err := inv.Client.AppsV1().Deployments(ns).Get(ctx, rep.Target, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return dep.Spec.Selector.MatchLabels, nil
	case "Pod":
		pod, err := inv.Client.CoreV1().Pods(ns).Get(ctx, rep.Target, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return pod.Labels, nil
	default:
		return nil, nil
	}
}

func selectorMatches(sel, podLabels map[string]string) bool {
	if len(sel) == 0 {
		return false
	}
	return labels.SelectorFromSet(sel).Matches(labels.Set(podLabels))
}

func countAddresses(ep *corev1.Endpoints) (ready, notReady int) {
	if ep == nil {
		return 0, 0
	}
	for _, subset := range ep.Subsets {
		ready += len(subset.Addresses)
		notReady += len(subset.NotReadyAddresses)
	}
	return ready, notReady
}

func formatPorts(ports []corev1.ServicePort) string {
	if len(ports) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ports))
	for _, p := range ports {
		parts = append(parts, fmt.Sprintf("%d/%s", p.Port, string(p.Protocol)))
	}
	return "ports " + strings.Join(parts, ",")
}

func mapExplainFindings(rep cluster.ExplainReport) []incident.Finding {
	out := make([]incident.Finding, 0, len(rep.Findings))
	for _, f := range rep.Findings {
		out = append(out, incident.Finding{
			Code:      f.Code,
			Severity:  mapSeverity(f.Severity),
			Title:     firstNonEmpty(f.Code, "Finding"),
			Message:   f.Message,
			Namespace: rep.Namespace,
		})
	}
	return out
}

func mapExplainEvidence(rep cluster.ExplainReport) []incident.EvidenceRef {
	var out []incident.EvidenceRef
	for _, step := range rep.Chain {
		out = append(out, incident.EvidenceRef{
			Type: incident.EvidenceObject,
			Resource: &incident.ResourceRef{
				Kind: step.Level, Name: step.Name, Namespace: rep.Namespace,
			},
			Reason:  step.Level,
			Message: step.Detail,
			Source:  "kubernetes",
		})
	}
	for _, ev := range rep.Events {
		out = append(out, incident.EvidenceRef{
			Type:    incident.EvidenceEvent,
			Reason:  "Event",
			Message: ev,
			Source:  "kubernetes",
			Resource: &incident.ResourceRef{
				Kind: rep.Kind, Name: rep.Target, Namespace: rep.Namespace,
			},
		})
	}
	if strings.TrimSpace(rep.LogTail) != "" {
		out = append(out, incident.EvidenceRef{
			Type:    incident.EvidenceLog,
			Reason:  "LogTail",
			Message: truncate(rep.LogTail, 512),
			Source:  "kubernetes",
			Resource: &incident.ResourceRef{
				Kind: "Pod", Name: firstNonEmpty(rep.LogPod, rep.Target), Namespace: rep.Namespace,
			},
		})
	}
	return out
}

func rootCauseFrom(rep cluster.ExplainReport, findings []incident.Finding) (string, float64) {
	priority := []string{"OOMKilled", "ImagePullBackOff", "ErrImagePull", "CrashLoopBackOff", "NoReadyEndpoints"}
	conf := map[string]float64{
		"OOMKilled":          0.85,
		"ImagePullBackOff":   0.85,
		"ErrImagePull":       0.85,
		"CrashLoopBackOff":   0.80,
		"NoReadyEndpoints":   0.80,
	}
	byCode := map[string]incident.Finding{}
	for _, f := range findings {
		byCode[f.Code] = f
	}
	for _, code := range priority {
		if f, ok := byCode[code]; ok {
			return f.Message, conf[code]
		}
	}
	if rep.Summary != "" {
		return rep.Summary, 0.55
	}
	return "Insufficient automated signal for a precise root cause", 0.45
}

func planHintFrom(rep cluster.ExplainReport) string {
	for _, f := range rep.Findings {
		switch f.Code {
		case "OOMKilled":
			return fmt.Sprintf("Suggested (approve required): raise memory for %s", rep.Target)
		case "CrashLoopBackOff":
			return fmt.Sprintf("Suggested: kprompt \"logs %s\" then fix start command / config", rep.Target)
		case "ImagePullBackOff", "ErrImagePull":
			return fmt.Sprintf("Suggested: verify image tag and imagePullSecrets for %s", rep.Target)
		}
	}
	return ""
}

func mapSeverity(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "error", "critical":
		return incident.SeverityHigh
	case "warning", "warn":
		return incident.SeverityMedium
	case "info":
		return incident.SeverityInfo
	default:
		return incident.SeverityMedium
	}
}

func prependChain(rep *cluster.ExplainReport, steps ...cluster.ChainStep) {
	if len(steps) == 0 {
		return
	}
	rep.Chain = append(steps, rep.Chain...)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
