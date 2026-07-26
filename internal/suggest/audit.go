package suggest

import (
	"context"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"

	"github.com/kprompt/kprompt/internal/incident"
	"github.com/kprompt/kprompt/internal/intent"
	"github.com/kprompt/kprompt/internal/planner"
)

// FromAudit turns hygiene findings into one aggregate, approve-gated harden plan
// plus guidance for findings kprompt will not auto-patch.
//
// The only auto-patched fixes REMOVE a privilege grant (privileged=false,
// allowPrivilegeEscalation=false) on Deployment containers — changes that never
// invent workload-specific values and never tighten a constraint that could stop
// a container from starting. Everything else (runAsNonRoot, host namespaces,
// image tags, resource requests/limits, non-Deployment kinds) stays guidance-only.
func FromAudit(ctx context.Context, client kubernetes.Interface, inv incident.Investigation) ([]Suggestion, error) {
	if client == nil {
		return nil, nil
	}

	type depKey struct{ name, ns string }
	patchable := map[depKey][]incident.Finding{}
	var order []depKey
	seenGuidance := map[string]bool{}
	var guidance []Suggestion

	for _, f := range inv.Findings {
		ref := auditResource(f)
		if ref == nil {
			continue
		}
		switch f.Code {
		case "Audit.Privileged", "Audit.PrivilegeEscalation":
			if ref.Kind != "Deployment" {
				addAuditGuidance(&guidance, seenGuidance, "Audit.NonDeployment",
					"Harden via workload manifest",
					fmt.Sprintf("harden %s/%s", strings.ToLower(ref.Kind), ref.Name),
					"StatefulSet/DaemonSet harden patches are not auto-generated yet — remove the privilege grant in your manifest")
				continue
			}
			k := depKey{ref.Name, ref.Namespace}
			if _, ok := patchable[k]; !ok {
				order = append(order, k)
			}
			patchable[k] = append(patchable[k], f)
		default:
			addAuditGuidance(&guidance, seenGuidance, f.Code, auditGuidanceTitle(f.Code), auditGuidancePrompt(f, ref), auditGuidanceSummary(f.Code))
		}
	}

	var actions []planner.Action
	for _, k := range order {
		act, err := hardenDeployment(ctx, client, k.name, k.ns, patchable[k])
		if err != nil {
			guidance = append(guidance, Suggestion{
				Code:    "Audit.Harden",
				Title:   "Harden Deployment",
				Prompt:  fmt.Sprintf("harden %s", k.name),
				Summary: fmt.Sprintf("Could not build a patch for Deployment/%s: %v", k.name, err),
			})
			continue
		}
		if act != nil {
			actions = append(actions, *act)
		}
	}

	var out []Suggestion
	if len(actions) > 0 {
		plan := &planner.ExecutionPlan{
			Intent: intent.Intent{
				Kind:   intent.KindPatch,
				Target: intent.Target{Kind: "Deployment", Namespace: inv.Namespace},
				Params: map[string]any{"reason": "AuditHarden"},
			},
			Actions:          actions,
			Summary:          fmt.Sprintf("Harden %d Deployment(s): remove privilege grants", len(actions)),
			RequiresApproval: true,
		}
		out = append(out, Suggestion{
			Code:    "Audit.Harden",
			Title:   "Remove privilege grants",
			Prompt:  "harden deployments",
			Plan:    plan,
			Summary: plan.Summary,
		})
	}
	out = append(out, guidance...)
	return out, nil
}

// hardenDeployment returns a single OpUpdate action removing privilege grants,
// or nil when the live spec already has nothing to remove.
func hardenDeployment(ctx context.Context, client kubernetes.Interface, name, ns string, findings []incident.Finding) (*planner.Action, error) {
	dep, err := client.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	patched := dep.DeepCopy()
	var changes []string
	for _, f := range findings {
		container := containerFromMessage(f.Title)
		for _, c := range matchContainers(patched, container) {
			switch f.Code {
			case "Audit.Privileged":
				if c.SecurityContext != nil && c.SecurityContext.Privileged != nil && *c.SecurityContext.Privileged {
					c.SecurityContext.Privileged = boolRef(false)
					changes = append(changes, fmt.Sprintf("%s: privileged=false", c.Name))
				}
			case "Audit.PrivilegeEscalation":
				if c.SecurityContext == nil || c.SecurityContext.AllowPrivilegeEscalation == nil || *c.SecurityContext.AllowPrivilegeEscalation {
					if c.SecurityContext == nil {
						c.SecurityContext = &corev1.SecurityContext{}
					}
					c.SecurityContext.AllowPrivilegeEscalation = boolRef(false)
					changes = append(changes, fmt.Sprintf("%s: allowPrivilegeEscalation=false", c.Name))
				}
			}
		}
	}
	if len(changes) == 0 {
		return nil, nil
	}
	patched.TypeMeta = metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"}
	raw, err := yaml.Marshal(patched)
	if err != nil {
		return nil, err
	}
	diff := fmt.Sprintf("~ Deployment/%s (update)\n  %s", dep.Name, strings.Join(changes, "\n  "))
	return &planner.Action{
		Op: planner.OpUpdate,
		Object: planner.ObjectRef{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
			Name:       dep.Name,
			Namespace:  dep.Namespace,
		},
		Manifest: string(raw),
		Diff:     diff,
	}, nil
}

// matchContainers returns pointers into the patched spec (init + main). When a
// container name is given it returns just that one; otherwise all containers so a
// privilege-removal applies broadly (always safe — it only drops a grant).
func matchContainers(dep *appsv1.Deployment, name string) []*corev1.Container {
	var all []*corev1.Container
	for i := range dep.Spec.Template.Spec.InitContainers {
		all = append(all, &dep.Spec.Template.Spec.InitContainers[i])
	}
	for i := range dep.Spec.Template.Spec.Containers {
		all = append(all, &dep.Spec.Template.Spec.Containers[i])
	}
	if name == "" {
		return all
	}
	for _, c := range all {
		if c.Name == name {
			return []*corev1.Container{c}
		}
	}
	return all
}

func auditResource(f incident.Finding) *incident.ResourceRef {
	for _, e := range f.Evidence {
		if e.Resource != nil && e.Resource.Name != "" {
			return e.Resource
		}
	}
	return nil
}

func addAuditGuidance(out *[]Suggestion, seen map[string]bool, code, title, prompt, summary string) {
	if seen[code] {
		return
	}
	seen[code] = true
	*out = append(*out, Suggestion{
		Code:    code,
		Title:   title,
		Prompt:  prompt,
		Summary: summary,
	})
}

func auditGuidanceTitle(code string) string {
	switch code {
	case "Audit.RunAsRoot":
		return "Run as non-root"
	case "Audit.HostNamespace":
		return "Drop host namespaces"
	case "Audit.LatestTag":
		return "Pin the image"
	case "Audit.MissingRequests":
		return "Set resource requests"
	case "Audit.MissingLimits":
		return "Set resource limits"
	case "Audit.MissingImagePullPolicy":
		return "Set imagePullPolicy"
	default:
		return "Review finding"
	}
}

func auditGuidancePrompt(f incident.Finding, ref *incident.ResourceRef) string {
	target := "workload"
	if ref != nil && ref.Name != "" {
		target = ref.Name
	}
	return fmt.Sprintf("describe %s", target)
}

func auditGuidanceSummary(code string) string {
	switch code {
	case "Audit.RunAsRoot":
		return "Set runAsNonRoot=true only with a non-root-capable image — enforcing it blindly can break container startup"
	case "Audit.HostNamespace":
		return "Remove hostNetwork/hostPID/hostIPC unless the workload genuinely needs the host namespace"
	case "Audit.LatestTag":
		return "Pin a specific tag or digest — kprompt never invents image tags"
	case "Audit.MissingRequests":
		return "Add resources.requests after profiling — CPU/memory values are workload-specific, not invented"
	case "Audit.MissingLimits":
		return "Add resources.limits after profiling — CPU/memory values are workload-specific, not invented"
	case "Audit.MissingImagePullPolicy":
		return "Set imagePullPolicy (e.g. IfNotPresent) once the image tag is pinned"
	default:
		return "Review the finding and remediate in your workload manifest"
	}
}

func boolRef(v bool) *bool { return &v }
