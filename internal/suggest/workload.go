package suggest

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/kprompt/kprompt/internal/cluster"
)

// workload is a deep-copied apps object ready to mutate and marshal.
type workload struct {
	Kind      string
	Name      string
	Namespace string
	Object    any
	PodSpec   *corev1.PodSpec
}

// resolveWorkload loads the owning Deployment / StatefulSet / DaemonSet for an
// explain target (workload name or Pod), returning a mutable deep copy.
func resolveWorkload(ctx context.Context, client kubernetes.Interface, rep cluster.ExplainReport) (*workload, error) {
	ns := rep.Namespace
	if ns == "" {
		ns = "default"
	}
	name := rep.Target
	kind := rep.Kind

	switch kind {
	case "Deployment", "StatefulSet", "DaemonSet":
		return loadWorkload(ctx, client, kind, name, ns)
	case "Pod":
		return resolveWorkloadFromPod(ctx, client, name, ns)
	case "":
		// Target may be a workload name or a Pod — try common kinds first.
		if w, err := loadWorkload(ctx, client, "Deployment", name, ns); err == nil {
			return w, nil
		}
		if w, err := loadWorkload(ctx, client, "StatefulSet", name, ns); err == nil {
			return w, nil
		}
		if w, err := loadWorkload(ctx, client, "DaemonSet", name, ns); err == nil {
			return w, nil
		}
		return resolveWorkloadFromPod(ctx, client, name, ns)
	default:
		return resolveWorkloadFromPod(ctx, client, name, ns)
	}
}

func loadWorkload(ctx context.Context, client kubernetes.Interface, kind, name, ns string) (*workload, error) {
	switch kind {
	case "Deployment":
		dep, err := client.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		patched := dep.DeepCopy()
		patched.TypeMeta = metav1.TypeMeta{APIVersion: "apps/v1", Kind: kind}
		return &workload{Kind: kind, Name: patched.Name, Namespace: patched.Namespace, Object: patched, PodSpec: &patched.Spec.Template.Spec}, nil
	case "StatefulSet":
		sts, err := client.AppsV1().StatefulSets(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		patched := sts.DeepCopy()
		patched.TypeMeta = metav1.TypeMeta{APIVersion: "apps/v1", Kind: kind}
		return &workload{Kind: kind, Name: patched.Name, Namespace: patched.Namespace, Object: patched, PodSpec: &patched.Spec.Template.Spec}, nil
	case "DaemonSet":
		ds, err := client.AppsV1().DaemonSets(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		patched := ds.DeepCopy()
		patched.TypeMeta = metav1.TypeMeta{APIVersion: "apps/v1", Kind: kind}
		return &workload{Kind: kind, Name: patched.Name, Namespace: patched.Namespace, Object: patched, PodSpec: &patched.Spec.Template.Spec}, nil
	default:
		return nil, fmt.Errorf("unsupported workload kind %s", kind)
	}
}

func resolveWorkloadFromPod(ctx context.Context, client kubernetes.Interface, name, ns string) (*workload, error) {
	pod, err := client.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	for _, ow := range pod.OwnerReferences {
		if ow.Controller == nil || !*ow.Controller {
			continue
		}
		switch ow.Kind {
		case "ReplicaSet":
			rs, err := client.AppsV1().ReplicaSets(ns).Get(ctx, ow.Name, metav1.GetOptions{})
			if err != nil {
				return nil, err
			}
			for _, row := range rs.OwnerReferences {
				if row.Kind == "Deployment" && row.Controller != nil && *row.Controller {
					return loadWorkload(ctx, client, "Deployment", row.Name, ns)
				}
			}
		case "StatefulSet":
			return loadWorkload(ctx, client, "StatefulSet", ow.Name, ns)
		case "DaemonSet":
			return loadWorkload(ctx, client, "DaemonSet", ow.Name, ns)
		}
	}
	return nil, fmt.Errorf("no owning Deployment/StatefulSet/DaemonSet for Pod/%s", name)
}

// resolveDeploymentContainer keeps the Deployment-only path used by CrashLoop
// rollback (rollout undo is Deployment-specific).
func resolveDeploymentContainer(ctx context.Context, client kubernetes.Interface, rep cluster.ExplainReport, container string) (*appsv1.Deployment, string, error) {
	w, err := resolveWorkload(ctx, client, rep)
	if err != nil || w == nil {
		return nil, container, err
	}
	if w.Kind != "Deployment" {
		return nil, container, fmt.Errorf("owning workload is %s, not Deployment", w.Kind)
	}
	dep, ok := w.Object.(*appsv1.Deployment)
	if !ok {
		return nil, container, fmt.Errorf("internal: expected *Deployment")
	}
	return dep, container, nil
}

func containerIndexInSpec(spec *corev1.PodSpec, name string) int {
	if name == "" {
		return 0
	}
	for i, c := range spec.Containers {
		if c.Name == name {
			return i
		}
	}
	return -1
}
