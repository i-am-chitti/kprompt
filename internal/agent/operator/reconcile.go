package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	agentv1 "github.com/kprompt/kprompt/api/v1"
)

const Finalizer = "kprompt.ai/agent-operator"

var gvr = schema.GroupVersionResource{
	Group:    agentv1.Group,
	Version:  agentv1.Version,
	Resource: "kpromptagents",
}

// Reconciler applies desired Observe workloads for a KpromptAgent.
type Reconciler struct {
	Kube    kubernetes.Interface
	Dynamic dynamic.Interface
	Options Options
}

// Reconcile ensures owned objects match the CR (or cleans up on delete).
func (r *Reconciler) Reconcile(ctx context.Context, cr *agentv1.KpromptAgent) error {
	if r == nil || r.Kube == nil || r.Dynamic == nil {
		return fmt.Errorf("operator: clients required")
	}
	if cr.DeletionTimestamp != nil {
		return r.cleanup(ctx, cr)
	}
	if err := r.ensureFinalizer(ctx, cr); err != nil {
		return err
	}
	desired, err := BuildDesired(cr, r.Options)
	if err != nil {
		_ = r.patchCondition(ctx, cr, "Ready", metav1.ConditionFalse, "InvalidSpec", err.Error())
		return err
	}
	sameNS := desired.WatchNamespace == cr.Namespace
	if err := r.applySA(ctx, desired.SA, sameNS); err != nil {
		return r.fail(ctx, cr, err)
	}
	if err := r.applyRole(ctx, desired.Role, sameNS); err != nil {
		return r.fail(ctx, cr, err)
	}
	if err := r.applyRoleBinding(ctx, desired.RoleBinding, sameNS); err != nil {
		return r.fail(ctx, cr, err)
	}
	if err := r.applyDeployment(ctx, desired.Deployment, sameNS); err != nil {
		return r.fail(ctx, cr, err)
	}
	return r.patchCondition(ctx, cr, "Ready", metav1.ConditionTrue, "Reconciled",
		fmt.Sprintf("Observe agent Deployment %s/%s ready for reconcile", desired.WatchNamespace, desired.Deployment.Name))
}

func (r *Reconciler) fail(ctx context.Context, cr *agentv1.KpromptAgent, err error) error {
	_ = r.patchCondition(ctx, cr, "Ready", metav1.ConditionFalse, "ReconcileError", err.Error())
	return err
}

func (r *Reconciler) cleanup(ctx context.Context, cr *agentv1.KpromptAgent) error {
	ns := WatchNamespace(cr)
	name := ResourceName(cr)
	_ = r.Kube.AppsV1().Deployments(ns).Delete(ctx, name, metav1.DeleteOptions{})
	_ = r.Kube.RbacV1().RoleBindings(ns).Delete(ctx, name, metav1.DeleteOptions{})
	_ = r.Kube.RbacV1().Roles(ns).Delete(ctx, name, metav1.DeleteOptions{})
	_ = r.Kube.CoreV1().ServiceAccounts(ns).Delete(ctx, name, metav1.DeleteOptions{})
	return r.removeFinalizer(ctx, cr)
}

func (r *Reconciler) ensureFinalizer(ctx context.Context, cr *agentv1.KpromptAgent) error {
	for _, f := range cr.Finalizers {
		if f == Finalizer {
			return nil
		}
	}
	cr.Finalizers = append(cr.Finalizers, Finalizer)
	return r.updateCR(ctx, cr)
}

func (r *Reconciler) removeFinalizer(ctx context.Context, cr *agentv1.KpromptAgent) error {
	out := cr.Finalizers[:0]
	for _, f := range cr.Finalizers {
		if f != Finalizer {
			out = append(out, f)
		}
	}
	cr.Finalizers = out
	return r.updateCR(ctx, cr)
}

func (r *Reconciler) updateCR(ctx context.Context, cr *agentv1.KpromptAgent) error {
	obj, err := toUnstructured(cr)
	if err != nil {
		return err
	}
	_, err = r.Dynamic.Resource(gvr).Namespace(cr.Namespace).Update(ctx, obj, metav1.UpdateOptions{})
	return err
}

func (r *Reconciler) patchCondition(ctx context.Context, cr *agentv1.KpromptAgent, typ string, status metav1.ConditionStatus, reason, message string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	body, err := json.Marshal(map[string]any{
		"status": map[string]any{
			"observedGeneration": cr.Generation,
			"conditions": []map[string]any{{
				"type":               typ,
				"status":             string(status),
				"reason":             reason,
				"message":            message,
				"lastTransitionTime": now,
			}},
		},
	})
	if err != nil {
		return err
	}
	_, err = r.Dynamic.Resource(gvr).Namespace(cr.Namespace).Patch(
		ctx, cr.Name, types.MergePatchType, body, metav1.PatchOptions{}, "status",
	)
	return err
}

func (r *Reconciler) applySA(ctx context.Context, sa *corev1.ServiceAccount, keepOwner bool) error {
	if !keepOwner {
		sa.OwnerReferences = nil
	}
	_, err := r.Kube.CoreV1().ServiceAccounts(sa.Namespace).Create(ctx, sa, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		cur, gerr := r.Kube.CoreV1().ServiceAccounts(sa.Namespace).Get(ctx, sa.Name, metav1.GetOptions{})
		if gerr != nil {
			return gerr
		}
		sa.ResourceVersion = cur.ResourceVersion
		_, err = r.Kube.CoreV1().ServiceAccounts(sa.Namespace).Update(ctx, sa, metav1.UpdateOptions{})
	}
	return err
}

func (r *Reconciler) applyRole(ctx context.Context, role *rbacv1.Role, keepOwner bool) error {
	if !keepOwner {
		role.OwnerReferences = nil
	}
	_, err := r.Kube.RbacV1().Roles(role.Namespace).Create(ctx, role, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		cur, gerr := r.Kube.RbacV1().Roles(role.Namespace).Get(ctx, role.Name, metav1.GetOptions{})
		if gerr != nil {
			return gerr
		}
		role.ResourceVersion = cur.ResourceVersion
		_, err = r.Kube.RbacV1().Roles(role.Namespace).Update(ctx, role, metav1.UpdateOptions{})
	}
	return err
}

func (r *Reconciler) applyRoleBinding(ctx context.Context, rb *rbacv1.RoleBinding, keepOwner bool) error {
	if !keepOwner {
		rb.OwnerReferences = nil
	}
	_, err := r.Kube.RbacV1().RoleBindings(rb.Namespace).Create(ctx, rb, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		cur, gerr := r.Kube.RbacV1().RoleBindings(rb.Namespace).Get(ctx, rb.Name, metav1.GetOptions{})
		if gerr != nil {
			return gerr
		}
		rb.ResourceVersion = cur.ResourceVersion
		_, err = r.Kube.RbacV1().RoleBindings(rb.Namespace).Update(ctx, rb, metav1.UpdateOptions{})
	}
	return err
}

func (r *Reconciler) applyDeployment(ctx context.Context, dep *appsv1.Deployment, keepOwner bool) error {
	if !keepOwner {
		dep.OwnerReferences = nil
	}
	_, err := r.Kube.AppsV1().Deployments(dep.Namespace).Create(ctx, dep, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		cur, gerr := r.Kube.AppsV1().Deployments(dep.Namespace).Get(ctx, dep.Name, metav1.GetOptions{})
		if gerr != nil {
			return gerr
		}
		dep.ResourceVersion = cur.ResourceVersion
		_, err = r.Kube.AppsV1().Deployments(dep.Namespace).Update(ctx, dep, metav1.UpdateOptions{})
	}
	return err
}

func toUnstructured(cr *agentv1.KpromptAgent) (*unstructured.Unstructured, error) {
	b, err := json.Marshal(cr)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	m["apiVersion"] = agentv1.Group + "/" + agentv1.Version
	m["kind"] = agentv1.Kind
	return &unstructured.Unstructured{Object: m}, nil
}

// FromUnstructured decodes a dynamic object into KpromptAgent.
func FromUnstructured(u *unstructured.Unstructured) (*agentv1.KpromptAgent, error) {
	b, err := u.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var cr agentv1.KpromptAgent
	if err := json.Unmarshal(b, &cr); err != nil {
		return nil, err
	}
	return &cr, nil
}
