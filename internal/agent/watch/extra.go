package watch

import (
	"context"
	"fmt"
	"strings"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
)

// AG-023 kinds (namespace Role-scoped). Node pressure is Event-derived — Nodes are
// cluster-scoped and are not watched here (ADR-0016 Role default).
const (
	ResourceService       = "Service"
	ResourceIngress       = "Ingress"
	ResourceHPA           = "HorizontalPodAutoscaler"
	ResourceResourceQuota = "ResourceQuota"
	ResourceLimitRange    = "LimitRange"
)

func init() {
	// Merge AG-023 aliases into the shared map.
	extra := map[string]string{
		"service": ResourceService, "services": ResourceService, "svc": ResourceService,
		"ingress": ResourceIngress, "ingresses": ResourceIngress, "ing": ResourceIngress,
		"horizontalpodautoscaler": ResourceHPA, "horizontalpodautoscalers": ResourceHPA,
		"hpa": ResourceHPA,
		"resourcequota": ResourceResourceQuota, "resourcequotas": ResourceResourceQuota, "quota": ResourceResourceQuota,
		"limitrange": ResourceLimitRange, "limitranges": ResourceLimitRange,
	}
	for k, v := range extra {
		resourceAliases[k] = v
	}
}

func (e *Engine) listWatchService(ctx context.Context, _ string) (string, watch.Interface, error) {
	ns := e.Options.Namespace
	list, err := e.Client.CoreV1().Services(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", nil, err
	}
	rv := list.ResourceVersion
	if e.Options.EmitInitial {
		for i := range list.Items {
			obj := list.Items[i]
			e.Handler(fromService(watch.Added, &obj))
		}
	}
	w, err := e.Client.CoreV1().Services(ns).Watch(ctx, metav1.ListOptions{ResourceVersion: rv, AllowWatchBookmarks: true})
	return rv, w, err
}

func (e *Engine) listWatchIngress(ctx context.Context, _ string) (string, watch.Interface, error) {
	ns := e.Options.Namespace
	list, err := e.Client.NetworkingV1().Ingresses(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", nil, err
	}
	rv := list.ResourceVersion
	if e.Options.EmitInitial {
		for i := range list.Items {
			obj := list.Items[i]
			e.Handler(fromIngress(watch.Added, &obj))
		}
	}
	w, err := e.Client.NetworkingV1().Ingresses(ns).Watch(ctx, metav1.ListOptions{ResourceVersion: rv, AllowWatchBookmarks: true})
	return rv, w, err
}

func (e *Engine) listWatchHPA(ctx context.Context, _ string) (string, watch.Interface, error) {
	ns := e.Options.Namespace
	list, err := e.Client.AutoscalingV2().HorizontalPodAutoscalers(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", nil, err
	}
	rv := list.ResourceVersion
	if e.Options.EmitInitial {
		for i := range list.Items {
			obj := list.Items[i]
			e.Handler(fromHPA(watch.Added, &obj))
		}
	}
	w, err := e.Client.AutoscalingV2().HorizontalPodAutoscalers(ns).Watch(ctx, metav1.ListOptions{ResourceVersion: rv, AllowWatchBookmarks: true})
	return rv, w, err
}

func (e *Engine) listWatchResourceQuota(ctx context.Context, _ string) (string, watch.Interface, error) {
	ns := e.Options.Namespace
	list, err := e.Client.CoreV1().ResourceQuotas(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", nil, err
	}
	rv := list.ResourceVersion
	if e.Options.EmitInitial {
		for i := range list.Items {
			obj := list.Items[i]
			e.Handler(fromResourceQuota(watch.Added, &obj))
		}
	}
	w, err := e.Client.CoreV1().ResourceQuotas(ns).Watch(ctx, metav1.ListOptions{ResourceVersion: rv, AllowWatchBookmarks: true})
	return rv, w, err
}

func (e *Engine) listWatchLimitRange(ctx context.Context, _ string) (string, watch.Interface, error) {
	ns := e.Options.Namespace
	list, err := e.Client.CoreV1().LimitRanges(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", nil, err
	}
	rv := list.ResourceVersion
	if e.Options.EmitInitial {
		for i := range list.Items {
			obj := list.Items[i]
			e.Handler(fromLimitRange(watch.Added, &obj))
		}
	}
	w, err := e.Client.CoreV1().LimitRanges(ns).Watch(ctx, metav1.ListOptions{ResourceVersion: rv, AllowWatchBookmarks: true})
	return rv, w, err
}

func fromService(t watch.EventType, s *corev1.Service) Event {
	return Event{
		Type:            t,
		Resource:        ResourceService,
		Namespace:       s.Namespace,
		Name:            s.Name,
		ResourceVersion: s.ResourceVersion,
		Detail:          fmt.Sprintf("type=%s clusterIP=%s ports=%d", s.Spec.Type, s.Spec.ClusterIP, len(s.Spec.Ports)),
		At:              time.Now().UTC(),
	}
}

func fromIngress(t watch.EventType, ing *networkingv1.Ingress) Event {
	hosts := make([]string, 0, len(ing.Spec.Rules))
	for _, r := range ing.Spec.Rules {
		if h := strings.TrimSpace(r.Host); h != "" {
			hosts = append(hosts, h)
		}
	}
	lbs := len(ing.Status.LoadBalancer.Ingress)
	detail := fmt.Sprintf("hosts=%s lb=%d", strings.Join(hosts, ","), lbs)
	if lbs == 0 && len(hosts) > 0 {
		detail += " pending_lb=true"
	}
	return Event{
		Type:            t,
		Resource:        ResourceIngress,
		Namespace:       ing.Namespace,
		Name:            ing.Name,
		ResourceVersion: ing.ResourceVersion,
		Detail:          detail,
		At:              time.Now().UTC(),
	}
}

func fromHPA(t watch.EventType, h *autoscalingv2.HorizontalPodAutoscaler) Event {
	max := h.Spec.MaxReplicas
	cur := h.Status.CurrentReplicas
	des := h.Status.DesiredReplicas
	detail := fmt.Sprintf("current=%d desired=%d max=%d", cur, des, max)
	if max > 0 && cur >= max {
		detail += " at_max=true"
	}
	reason := ""
	for _, c := range h.Status.Conditions {
		if c.Status == corev1.ConditionFalse && (c.Type == autoscalingv2.AbleToScale || c.Type == autoscalingv2.ScalingLimited) {
			reason = string(c.Type)
			if c.Reason != "" {
				detail += " condition=" + c.Reason
			}
		}
	}
	return Event{
		Type:            t,
		Resource:        ResourceHPA,
		Namespace:       h.Namespace,
		Name:            h.Name,
		ResourceVersion: h.ResourceVersion,
		Reason:          reason,
		Detail:          detail,
		At:              time.Now().UTC(),
	}
}

func fromResourceQuota(t watch.EventType, q *corev1.ResourceQuota) Event {
	hard := q.Status.Hard
	used := q.Status.Used
	exceeded := false
	for name, hQty := range hard {
		if u, ok := used[name]; ok && u.Cmp(hQty) >= 0 {
			exceeded = true
			break
		}
	}
	detail := fmt.Sprintf("hard=%d used=%d", len(hard), len(used))
	if exceeded {
		detail += " exceeded=true"
	}
	return Event{
		Type:            t,
		Resource:        ResourceResourceQuota,
		Namespace:       q.Namespace,
		Name:            q.Name,
		ResourceVersion: q.ResourceVersion,
		Detail:          detail,
		At:              time.Now().UTC(),
	}
}

func fromLimitRange(t watch.EventType, lr *corev1.LimitRange) Event {
	return Event{
		Type:            t,
		Resource:        ResourceLimitRange,
		Namespace:       lr.Namespace,
		Name:            lr.Name,
		ResourceVersion: lr.ResourceVersion,
		Detail:          fmt.Sprintf("limits=%d", len(lr.Spec.Limits)),
		At:              time.Now().UTC(),
	}
}
