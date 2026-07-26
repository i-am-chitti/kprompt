package suggest

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"

	"github.com/kprompt/kprompt/internal/cluster"
	"github.com/kprompt/kprompt/internal/incident"
	"github.com/kprompt/kprompt/internal/intent"
	"github.com/kprompt/kprompt/internal/planner"
)

// Suggestion is a follow-up action derived from explain / why findings.
type Suggestion struct {
	Code    string
	Title   string
	Prompt  string // copy-pasteable follow-up prompt
	Plan    *planner.ExecutionPlan
	Summary string
}

// FromExplain maps explain findings to prompts and optional mutation plans.
// Actionable plans still require approval to apply.
// prompt is used only when a finding needs user-supplied details (e.g. a replacement image).
func FromExplain(ctx context.Context, client kubernetes.Interface, rep cluster.ExplainReport, prompt string) ([]Suggestion, error) {
	if client == nil {
		return nil, nil
	}
	var out []Suggestion
	seen := map[string]bool{}
	for _, f := range rep.Findings {
		key := f.Code + "|" + f.Container
		if seen[key] {
			continue
		}
		seen[key] = true
		switch f.Code {
		case "OOMKilled":
			s, err := suggestOOM(ctx, client, rep, f)
			if err != nil {
				return out, err
			}
			if s != nil {
				out = append(out, *s)
			}
		case "CrashLoopBackOff":
			s, err := suggestCrashLoop(ctx, client, rep, f)
			if err != nil {
				return out, err
			}
			if s != nil {
				out = append(out, *s)
			}
		case "ImagePullBackOff", "ErrImagePull":
			s, err := suggestImagePull(ctx, client, rep, f, prompt)
			if err != nil {
				return out, err
			}
			if s != nil {
				out = append(out, *s)
			}
		}
	}
	return out, nil
}

// FromInvestigation maps ADR-0014 why/investigate-style findings onto the same suggest path.
func FromInvestigation(ctx context.Context, client kubernetes.Interface, inv incident.Investigation, prompt string) ([]Suggestion, error) {
	return FromExplain(ctx, client, ExplainReportFromInvestigation(inv), prompt)
}

// ExplainReportFromInvestigation converts Investigation finding codes into explain-lite Findings.
func ExplainReportFromInvestigation(inv incident.Investigation) cluster.ExplainReport {
	rep := cluster.ExplainReport{Namespace: inv.Namespace}
	if inv.Target != nil {
		rep.Target = inv.Target.Name
		rep.Kind = inv.Target.Kind
		if inv.Target.Namespace != "" {
			rep.Namespace = inv.Target.Namespace
		}
	}
	// Dedupe by mapped code so Symptom.* + Cause.* for the same failure
	// do not produce duplicate suggest plans (e.g. ImagePull with empty container).
	seen := map[string]int{} // code → index in Findings
	for _, f := range inv.Findings {
		code := mapInvestigationCode(f.Code)
		if code == "" {
			continue
		}
		container := containerFromMessage(f.Message)
		if idx, ok := seen[code]; ok {
			if container != "" && rep.Findings[idx].Container == "" {
				rep.Findings[idx].Container = container
			}
			continue
		}
		seen[code] = len(rep.Findings)
		rep.Findings = append(rep.Findings, cluster.Finding{
			Severity:  "error",
			Code:      code,
			Message:   f.Message,
			Container: container,
		})
	}
	return rep
}

func mapInvestigationCode(code string) string {
	switch code {
	case "Symptom.CrashLoop", "Cause.ExitNonZero":
		return "CrashLoopBackOff"
	case "Symptom.ImagePull", "Cause.BadImageRef":
		return "ImagePullBackOff"
	case "Symptom.OOM", "Cause.OOMKilled", "Cause.MemoryLimit":
		return "OOMKilled"
	default:
		return ""
	}
}

var containerMsgRE = regexp.MustCompile(`(?i)container\s+([a-z0-9][-a-z0-9]*)`)

func containerFromMessage(msg string) string {
	m := containerMsgRE.FindStringSubmatch(msg)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

func suggestOOM(ctx context.Context, client kubernetes.Interface, rep cluster.ExplainReport, f cluster.Finding) (*Suggestion, error) {
	dep, container, err := resolveDeploymentContainer(ctx, client, rep, f.Container)
	if err != nil || dep == nil {
		return &Suggestion{
			Code:    "OOMKilled",
			Title:   "Raise memory limit",
			Prompt:  fmt.Sprintf(`raise memory for %s`, rep.Target),
			Summary: "Could not load Deployment for an auto-plan; try a manual resource patch",
		}, nil
	}
	idx := containerIndex(dep, container)
	if idx < 0 {
		idx = 0
		container = dep.Spec.Template.Spec.Containers[0].Name
	}
	oldLimit, newLimit := bumpMemory(dep.Spec.Template.Spec.Containers[idx].Resources.Limits)
	oldReq, newReq := bumpMemory(dep.Spec.Template.Spec.Containers[idx].Resources.Requests)

	patched := dep.DeepCopy()
	c := &patched.Spec.Template.Spec.Containers[idx]
	if c.Resources.Limits == nil {
		c.Resources.Limits = corev1.ResourceList{}
	}
	if c.Resources.Requests == nil {
		c.Resources.Requests = corev1.ResourceList{}
	}
	c.Resources.Limits[corev1.ResourceMemory] = newLimit
	if !oldReq.IsZero() || !dep.Spec.Template.Spec.Containers[idx].Resources.Requests.Memory().IsZero() {
		c.Resources.Requests[corev1.ResourceMemory] = newReq
	} else {
		req := newLimit.DeepCopy()
		if v := newLimit.Value(); v > 0 {
			req.Set(v / 2)
		}
		c.Resources.Requests[corev1.ResourceMemory] = req
	}
	patched.TypeMeta = metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"}
	raw, err := yaml.Marshal(patched)
	if err != nil {
		return nil, err
	}

	diff := fmt.Sprintf("~ Deployment/%s (update)\n  container: %s\n  memory limit: %s → %s",
		dep.Name, container, qtyString(oldLimit), qtyString(newLimit))
	plan := &planner.ExecutionPlan{
		Intent: intent.Intent{
			Kind: intent.KindPatch,
			Target: intent.Target{
				Name:      dep.Name,
				Namespace: dep.Namespace,
				Kind:      "Deployment",
			},
			Params: map[string]any{
				"reason":    "OOMKilled",
				"container": container,
				"memory":    newLimit.String(),
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
		Summary:          fmt.Sprintf("Raise memory limit on Deployment/%s container %s (%s → %s)", dep.Name, container, qtyString(oldLimit), qtyString(newLimit)),
		RequiresApproval: true,
	}
	return &Suggestion{
		Code:    "OOMKilled",
		Title:   "Raise memory limit",
		Prompt:  fmt.Sprintf(`raise memory for %s to %s`, dep.Name, newLimit.String()),
		Plan:    plan,
		Summary: plan.Summary,
	}, nil
}

func suggestCrashLoop(ctx context.Context, client kubernetes.Interface, rep cluster.ExplainReport, f cluster.Finding) (*Suggestion, error) {
	logsOnly := &Suggestion{
		Code:    f.Code,
		Title:   "Inspect crash logs",
		Prompt:  fmt.Sprintf(`logs %s`, rep.Target),
		Summary: "Fetch recent container logs to see why the process exits",
	}
	dep, _, err := resolveDeploymentContainer(ctx, client, rep, f.Container)
	if err != nil || dep == nil {
		return logsOnly, nil
	}
	ok, err := deploymentHasPriorRevision(ctx, client, dep)
	if err != nil || !ok {
		return logsOnly, nil
	}
	plan := &planner.ExecutionPlan{
		Intent: intent.Intent{
			Kind: intent.KindRollback,
			Target: intent.Target{
				Name:      dep.Name,
				Namespace: dep.Namespace,
				Kind:      "Deployment",
			},
			Params: map[string]any{"reason": "CrashLoopBackOff"},
		},
		Actions: []planner.Action{{
			Op: planner.OpRollback,
			Object: planner.ObjectRef{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       dep.Name,
				Namespace:  dep.Namespace,
			},
			Diff: fmt.Sprintf("rollout undo Deployment/%s", dep.Name),
		}},
		Summary:          fmt.Sprintf("Rollback Deployment/%s in %s to previous revision", dep.Name, dep.Namespace),
		RequiresApproval: true,
	}
	return &Suggestion{
		Code:    f.Code,
		Title:   "Roll back Deployment",
		Prompt:  fmt.Sprintf(`rollback %s`, dep.Name),
		Plan:    plan,
		Summary: plan.Summary,
	}, nil
}

func suggestImagePull(ctx context.Context, client kubernetes.Interface, rep cluster.ExplainReport, f cluster.Finding, prompt string) (*Suggestion, error) {
	guidance := &Suggestion{
		Code:    f.Code,
		Title:   "Check image name / pull secrets",
		Prompt:  fmt.Sprintf(`describe %s`, rep.Target),
		Summary: "Verify the image reference and registry credentials — no replacement tag was named in the prompt",
	}
	newImage := extractReplacementImage(prompt)
	if newImage == "" {
		return guidance, nil
	}
	dep, container, err := resolveDeploymentContainer(ctx, client, rep, f.Container)
	if err != nil || dep == nil {
		return &Suggestion{
			Code:    f.Code,
			Title:   "Set container image",
			Prompt:  fmt.Sprintf(`set %s image to %s`, rep.Target, newImage),
			Summary: "Could not load Deployment for an auto-plan; try a manual image patch",
		}, nil
	}
	idx := containerIndex(dep, container)
	if idx < 0 {
		idx = 0
		container = dep.Spec.Template.Spec.Containers[0].Name
	}
	oldImage := dep.Spec.Template.Spec.Containers[idx].Image
	if oldImage == newImage {
		return guidance, nil
	}
	patched := dep.DeepCopy()
	patched.Spec.Template.Spec.Containers[idx].Image = newImage
	patched.TypeMeta = metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"}
	raw, err := yaml.Marshal(patched)
	if err != nil {
		return nil, err
	}
	diff := fmt.Sprintf("~ Deployment/%s (update)\n  container: %s\n  image: %s → %s",
		dep.Name, container, oldImage, newImage)
	plan := &planner.ExecutionPlan{
		Intent: intent.Intent{
			Kind: intent.KindPatch,
			Target: intent.Target{
				Name:      dep.Name,
				Namespace: dep.Namespace,
				Kind:      "Deployment",
			},
			Params: map[string]any{
				"reason":    f.Code,
				"container": container,
				"image":     newImage,
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
		Summary:          fmt.Sprintf("Set image on Deployment/%s container %s (%s → %s)", dep.Name, container, oldImage, newImage),
		RequiresApproval: true,
	}
	return &Suggestion{
		Code:    f.Code,
		Title:   "Set container image",
		Prompt:  fmt.Sprintf(`set %s image to %s`, dep.Name, newImage),
		Plan:    plan,
		Summary: plan.Summary,
	}, nil
}

var (
	imageToRE  = regexp.MustCompile(`(?i)image\s+to\s+["']?([a-z0-9][a-z0-9._/-]*(?::[a-zA-Z0-9._-]+)?)["']?`)
	imageRefRE = regexp.MustCompile(`(?i)\b((?:ghcr\.io|gcr\.io|quay\.io|registry\.k8s\.io|[a-z0-9.-]+\.[a-z]{2,}/)[a-z0-9._/-]+(?::[a-zA-Z0-9._-]+)?|[a-z0-9]+/[a-z0-9._/-]+(?::[a-zA-Z0-9._-]+))\b`)
)

// extractReplacementImage returns a user-named image from the prompt, or empty.
// Never invents tags — ImagePull plans require an explicit replacement.
func extractReplacementImage(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ""
	}
	if m := imageToRE.FindStringSubmatch(prompt); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	// Prefer the last image-looking ref so "fix bad → set good" picks the replacement.
	matches := imageRefRE.FindAllString(prompt, -1)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1]
}

func deploymentHasPriorRevision(ctx context.Context, client kubernetes.Interface, dep *appsv1.Deployment) (bool, error) {
	rss, err := client.AppsV1().ReplicaSets(dep.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, err
	}
	type revRS struct {
		rev int
	}
	var owned []revRS
	for i := range rss.Items {
		rs := &rss.Items[i]
		if !ownedByDeployment(rs, dep) {
			continue
		}
		rev := revisionOf(rs)
		if rev > 0 {
			owned = append(owned, revRS{rev: rev})
		}
	}
	if len(owned) < 2 {
		return false, nil
	}
	sort.Slice(owned, func(i, j int) bool { return owned[i].rev < owned[j].rev })
	return true, nil
}

func ownedByDeployment(rs *appsv1.ReplicaSet, dep *appsv1.Deployment) bool {
	for _, ow := range rs.OwnerReferences {
		if ow.Kind == "Deployment" && ow.Name == dep.Name {
			if ow.Controller != nil && *ow.Controller {
				return true
			}
			if ow.UID != "" && dep.UID != "" && ow.UID == dep.UID {
				return true
			}
			if ow.UID == "" {
				return true
			}
		}
	}
	return false
}

func revisionOf(rs *appsv1.ReplicaSet) int {
	if rs.Annotations == nil {
		return 0
	}
	s := rs.Annotations["deployment.kubernetes.io/revision"]
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

func resolveDeploymentContainer(ctx context.Context, client kubernetes.Interface, rep cluster.ExplainReport, container string) (*appsv1.Deployment, string, error) {
	ns := rep.Namespace
	if ns == "" {
		ns = "default"
	}
	name := rep.Target
	if rep.Kind == "Deployment" || rep.Kind == "" {
		dep, err := client.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			return dep, container, nil
		}
	}
	pod, err := client.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, container, err
	}
	for _, ow := range pod.OwnerReferences {
		if ow.Kind == "ReplicaSet" && ow.Controller != nil && *ow.Controller {
			rs, err := client.AppsV1().ReplicaSets(ns).Get(ctx, ow.Name, metav1.GetOptions{})
			if err != nil {
				return nil, container, err
			}
			for _, row := range rs.OwnerReferences {
				if row.Kind == "Deployment" && row.Controller != nil && *row.Controller {
					dep, err := client.AppsV1().Deployments(ns).Get(ctx, row.Name, metav1.GetOptions{})
					return dep, container, err
				}
			}
		}
	}
	return nil, container, fmt.Errorf("no owning Deployment for Pod/%s", name)
}

func containerIndex(dep *appsv1.Deployment, name string) int {
	if name == "" {
		return 0
	}
	for i, c := range dep.Spec.Template.Spec.Containers {
		if c.Name == name {
			return i
		}
	}
	return -1
}

func bumpMemory(list corev1.ResourceList) (oldQty, newQty resource.Quantity) {
	if list != nil {
		if q, ok := list[corev1.ResourceMemory]; ok && !q.IsZero() {
			oldQty = q.DeepCopy()
			v := q.Value()
			if v <= 0 {
				newQty = resource.MustParse("256Mi")
				return oldQty, newQty
			}
			newQty = *resource.NewQuantity(v*2, resource.BinarySI)
			return oldQty, newQty
		}
	}
	newQty = resource.MustParse("256Mi")
	return oldQty, newQty
}

func qtyString(q resource.Quantity) string {
	if q.IsZero() {
		return "(none)"
	}
	return q.String()
}

// ActionablePlans returns suggestions that carry an ExecutionPlan.
func ActionablePlans(suggestions []Suggestion) []Suggestion {
	var out []Suggestion
	for _, s := range suggestions {
		if s.Plan != nil {
			out = append(out, s)
		}
	}
	return out
}

// FormatPromptHint returns a shell-friendly follow-up example.
func FormatPromptHint(s Suggestion) string {
	p := strings.TrimSpace(s.Prompt)
	if p == "" {
		return ""
	}
	return fmt.Sprintf(`kprompt "%s" --approve`, p)
}
