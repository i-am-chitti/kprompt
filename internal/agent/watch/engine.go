// Package watch implements namespace-scoped watching for the Observe / Namespace Agent (AG-003 · AG-004 · AG-023).
//
// It uses Kubernetes list→watch with reconnect/backoff and optional bookmark events.
// This package is read-only: it never mutates the cluster.
// Node pressure is derived from Events (Role-scoped); cluster-scoped Node watch is out of scope.
package watch

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
)

// Canonical resource kinds the engine can watch (AG-003 · AG-004).
const (
	ResourcePod         = "Pod"
	ResourceEvent       = "Event"
	ResourceDeployment  = "Deployment"
	ResourceReplicaSet  = "ReplicaSet"
	ResourceStatefulSet = "StatefulSet"
	ResourceJob         = "Job"
	ResourceCronJob     = "CronJob"
	ResourcePVC         = "PersistentVolumeClaim"
	ResourceConfigMap   = "ConfigMap"
	ResourceSecret      = "Secret"

	defaultMinBackoff = 500 * time.Millisecond
	defaultMaxBackoff = 30 * time.Second
)

// Event is a normalized watch notification for agent pipelines.
type Event struct {
	Type            watch.EventType `json:"type"`
	Resource        string          `json:"resource"` // Pod | Event | Deployment | …
	Namespace       string          `json:"namespace"`
	Name            string          `json:"name"`
	ResourceVersion string          `json:"resourceVersion,omitempty"`
	Reason          string          `json:"reason,omitempty"`  // Event.reason or Pod condition
	Message         string          `json:"message,omitempty"` // Event.message
	InvolvedKind    string          `json:"involvedKind,omitempty"`
	InvolvedName    string          `json:"involvedName,omitempty"`
	PodPhase        string          `json:"podPhase,omitempty"`
	Detail          string          `json:"detail,omitempty"` // resource-specific summary (e.g. 2/3 ready)
	At              time.Time       `json:"at"`
}

// Handler receives normalized events. Must be safe for concurrent calls from pod/event loops.
type Handler func(Event)

// resourceAliases maps user / CRD input (lowercase plural or Kind) to canonical kinds.
var resourceAliases = map[string]string{
	"pod": ResourcePod, "pods": ResourcePod,
	"event": ResourceEvent, "events": ResourceEvent,
	"deployment": ResourceDeployment, "deployments": ResourceDeployment, "deploy": ResourceDeployment,
	"replicaset": ResourceReplicaSet, "replicasets": ResourceReplicaSet, "rs": ResourceReplicaSet,
	"statefulset": ResourceStatefulSet, "statefulsets": ResourceStatefulSet, "sts": ResourceStatefulSet,
	"job": ResourceJob, "jobs": ResourceJob,
	"cronjob": ResourceCronJob, "cronjobs": ResourceCronJob, "cj": ResourceCronJob,
	"persistentvolumeclaim": ResourcePVC, "persistentvolumeclaims": ResourcePVC, "pvc": ResourcePVC,
	"configmap": ResourceConfigMap, "configmaps": ResourceConfigMap, "cm": ResourceConfigMap,
	"secret": ResourceSecret, "secrets": ResourceSecret,
}

// NormalizeResources maps free-form names to canonical kinds, dropping unknowns
// and de-duplicating. Empty input → Pods + Events. Secrets stay opt-in: they are
// only included when explicitly requested (never added implicitly).
func NormalizeResources(in []string) []string {
	if len(in) == 0 {
		return []string{ResourcePod, ResourceEvent}
	}
	seen := map[string]bool{}
	var out []string
	for _, raw := range in {
		key := strings.ToLower(strings.TrimSpace(raw))
		canon, ok := resourceAliases[key]
		if !ok || seen[canon] {
			continue
		}
		seen[canon] = true
		out = append(out, canon)
	}
	if len(out) == 0 {
		return []string{ResourcePod, ResourceEvent}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Options configures the watch engine.
type Options struct {
	Namespace string
	// Resources defaults to Pods + Events when empty.
	Resources []string
	// EmitInitial sends Listed objects as Added before watching (cold-start sync).
	EmitInitial bool
	MinBackoff  time.Duration
	MaxBackoff  time.Duration
}

// Engine watches one namespace via client-go.
type Engine struct {
	Client  kubernetes.Interface
	Options Options
	Handler Handler
}

// Run blocks until ctx is cancelled. Returns ctx.Err() on clean cancel.
func (e *Engine) Run(ctx context.Context) error {
	if e == nil || e.Client == nil {
		return fmt.Errorf("watch: client is required")
	}
	if e.Handler == nil {
		return fmt.Errorf("watch: handler is required")
	}
	ns := e.Options.Namespace
	if ns == "" {
		return fmt.Errorf("watch: namespace is required")
	}
	resources := NormalizeResources(e.Options.Resources)
	minB := e.Options.MinBackoff
	if minB <= 0 {
		minB = defaultMinBackoff
	}
	maxB := e.Options.MaxBackoff
	if maxB <= 0 {
		maxB = defaultMaxBackoff
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(resources))

	for _, res := range resources {
		res := res
		wg.Add(1)
		go func() {
			defer wg.Done()
			var loopErr error
			if lw := e.listWatchFor(res); lw != nil {
				loopErr = e.loop(ctx, res, minB, maxB, lw)
			} else {
				loopErr = fmt.Errorf("watch: unsupported resource %q", res)
			}
			if loopErr != nil && ctx.Err() == nil {
				errCh <- loopErr
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		<-done
		return ctx.Err()
	case err := <-errCh:
		return err
	case <-done:
		return nil
	}
}

type listWatchFn func(ctx context.Context, resourceVersion string) (rv string, w watch.Interface, err error)

func (e *Engine) loop(ctx context.Context, resource string, minB, maxB time.Duration, lw listWatchFn) error {
	backoff := minB
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		rv, w, err := lw(ctx, "")
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err := sleepBackoff(ctx, backoff); err != nil {
				return err
			}
			backoff = nextBackoff(backoff, maxB)
			continue
		}
		backoff = minB
		err = e.consume(ctx, resource, rv, w)
		w.Stop()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			// reconnect after transient watch failure
		}
		if err := sleepBackoff(ctx, backoff); err != nil {
			return err
		}
		backoff = nextBackoff(backoff, maxB)
	}
}

func (e *Engine) consume(ctx context.Context, resource, _ string, w watch.Interface) error {
	ch := w.ResultChan()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case we, ok := <-ch:
			if !ok {
				return fmt.Errorf("watch: %s channel closed", resource)
			}
			if we.Type == watch.Error {
				return fmt.Errorf("watch: %s error event", resource)
			}
			if we.Type == watch.Bookmark {
				continue
			}
			ev, ok := normalize(resource, we)
			if !ok {
				continue
			}
			e.Handler(ev)
		}
	}
}

func (e *Engine) listWatchFor(resource string) listWatchFn {
	switch resource {
	case ResourcePod:
		return e.listWatchPods
	case ResourceEvent:
		return e.listWatchEvents
	case ResourceDeployment:
		return e.listWatchDeployments
	case ResourceReplicaSet:
		return e.listWatchReplicaSets
	case ResourceStatefulSet:
		return e.listWatchStatefulSets
	case ResourceJob:
		return e.listWatchJobs
	case ResourceCronJob:
		return e.listWatchCronJobs
	case ResourcePVC:
		return e.listWatchPVCs
	case ResourceConfigMap:
		return e.listWatchConfigMaps
	case ResourceSecret:
		return e.listWatchSecrets
	case ResourceService:
		return e.listWatchService
	case ResourceIngress:
		return e.listWatchIngress
	case ResourceHPA:
		return e.listWatchHPA
	case ResourceResourceQuota:
		return e.listWatchResourceQuota
	case ResourceLimitRange:
		return e.listWatchLimitRange
	default:
		return nil
	}
}

func (e *Engine) listWatchPods(ctx context.Context, _ string) (string, watch.Interface, error) {
	ns := e.Options.Namespace
	list, err := e.Client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", nil, err
	}
	rv := list.ResourceVersion
	if e.Options.EmitInitial {
		for i := range list.Items {
			pod := list.Items[i]
			e.Handler(fromPod(watch.Added, &pod))
		}
	}
	w, err := e.Client.CoreV1().Pods(ns).Watch(ctx, metav1.ListOptions{
		ResourceVersion:     rv,
		AllowWatchBookmarks: true,
	})
	return rv, w, err
}

func (e *Engine) listWatchEvents(ctx context.Context, _ string) (string, watch.Interface, error) {
	ns := e.Options.Namespace
	list, err := e.Client.CoreV1().Events(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", nil, err
	}
	rv := list.ResourceVersion
	if e.Options.EmitInitial {
		for i := range list.Items {
			ev := list.Items[i]
			e.Handler(fromEvent(watch.Added, &ev))
		}
	}
	w, err := e.Client.CoreV1().Events(ns).Watch(ctx, metav1.ListOptions{
		ResourceVersion:     rv,
		AllowWatchBookmarks: true,
	})
	return rv, w, err
}

func (e *Engine) listWatchDeployments(ctx context.Context, _ string) (string, watch.Interface, error) {
	ns := e.Options.Namespace
	list, err := e.Client.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", nil, err
	}
	rv := list.ResourceVersion
	if e.Options.EmitInitial {
		for i := range list.Items {
			obj := list.Items[i]
			e.Handler(fromDeployment(watch.Added, &obj))
		}
	}
	w, err := e.Client.AppsV1().Deployments(ns).Watch(ctx, metav1.ListOptions{ResourceVersion: rv, AllowWatchBookmarks: true})
	return rv, w, err
}

func (e *Engine) listWatchReplicaSets(ctx context.Context, _ string) (string, watch.Interface, error) {
	ns := e.Options.Namespace
	list, err := e.Client.AppsV1().ReplicaSets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", nil, err
	}
	rv := list.ResourceVersion
	if e.Options.EmitInitial {
		for i := range list.Items {
			obj := list.Items[i]
			e.Handler(fromReplicaSet(watch.Added, &obj))
		}
	}
	w, err := e.Client.AppsV1().ReplicaSets(ns).Watch(ctx, metav1.ListOptions{ResourceVersion: rv, AllowWatchBookmarks: true})
	return rv, w, err
}

func (e *Engine) listWatchStatefulSets(ctx context.Context, _ string) (string, watch.Interface, error) {
	ns := e.Options.Namespace
	list, err := e.Client.AppsV1().StatefulSets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", nil, err
	}
	rv := list.ResourceVersion
	if e.Options.EmitInitial {
		for i := range list.Items {
			obj := list.Items[i]
			e.Handler(fromStatefulSet(watch.Added, &obj))
		}
	}
	w, err := e.Client.AppsV1().StatefulSets(ns).Watch(ctx, metav1.ListOptions{ResourceVersion: rv, AllowWatchBookmarks: true})
	return rv, w, err
}

func (e *Engine) listWatchJobs(ctx context.Context, _ string) (string, watch.Interface, error) {
	ns := e.Options.Namespace
	list, err := e.Client.BatchV1().Jobs(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", nil, err
	}
	rv := list.ResourceVersion
	if e.Options.EmitInitial {
		for i := range list.Items {
			obj := list.Items[i]
			e.Handler(fromJob(watch.Added, &obj))
		}
	}
	w, err := e.Client.BatchV1().Jobs(ns).Watch(ctx, metav1.ListOptions{ResourceVersion: rv, AllowWatchBookmarks: true})
	return rv, w, err
}

func (e *Engine) listWatchCronJobs(ctx context.Context, _ string) (string, watch.Interface, error) {
	ns := e.Options.Namespace
	list, err := e.Client.BatchV1().CronJobs(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", nil, err
	}
	rv := list.ResourceVersion
	if e.Options.EmitInitial {
		for i := range list.Items {
			obj := list.Items[i]
			e.Handler(fromCronJob(watch.Added, &obj))
		}
	}
	w, err := e.Client.BatchV1().CronJobs(ns).Watch(ctx, metav1.ListOptions{ResourceVersion: rv, AllowWatchBookmarks: true})
	return rv, w, err
}

func (e *Engine) listWatchPVCs(ctx context.Context, _ string) (string, watch.Interface, error) {
	ns := e.Options.Namespace
	list, err := e.Client.CoreV1().PersistentVolumeClaims(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", nil, err
	}
	rv := list.ResourceVersion
	if e.Options.EmitInitial {
		for i := range list.Items {
			obj := list.Items[i]
			e.Handler(fromPVC(watch.Added, &obj))
		}
	}
	w, err := e.Client.CoreV1().PersistentVolumeClaims(ns).Watch(ctx, metav1.ListOptions{ResourceVersion: rv, AllowWatchBookmarks: true})
	return rv, w, err
}

func (e *Engine) listWatchConfigMaps(ctx context.Context, _ string) (string, watch.Interface, error) {
	ns := e.Options.Namespace
	list, err := e.Client.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", nil, err
	}
	rv := list.ResourceVersion
	if e.Options.EmitInitial {
		for i := range list.Items {
			obj := list.Items[i]
			e.Handler(fromConfigMap(watch.Added, &obj))
		}
	}
	w, err := e.Client.CoreV1().ConfigMaps(ns).Watch(ctx, metav1.ListOptions{ResourceVersion: rv, AllowWatchBookmarks: true})
	return rv, w, err
}

// listWatchSecrets watches Secret metadata only (never values). Opt-in (ADR-0013).
func (e *Engine) listWatchSecrets(ctx context.Context, _ string) (string, watch.Interface, error) {
	ns := e.Options.Namespace
	list, err := e.Client.CoreV1().Secrets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", nil, err
	}
	rv := list.ResourceVersion
	if e.Options.EmitInitial {
		for i := range list.Items {
			obj := list.Items[i]
			e.Handler(fromSecret(watch.Added, &obj))
		}
	}
	w, err := e.Client.CoreV1().Secrets(ns).Watch(ctx, metav1.ListOptions{ResourceVersion: rv, AllowWatchBookmarks: true})
	return rv, w, err
}

func normalize(resource string, we watch.Event) (Event, bool) {
	switch resource {
	case ResourcePod:
		obj, ok := we.Object.(*corev1.Pod)
		if !ok {
			return Event{}, false
		}
		return fromPod(we.Type, obj), true
	case ResourceEvent:
		obj, ok := we.Object.(*corev1.Event)
		if !ok {
			return Event{}, false
		}
		return fromEvent(we.Type, obj), true
	case ResourceDeployment:
		obj, ok := we.Object.(*appsv1.Deployment)
		if !ok {
			return Event{}, false
		}
		return fromDeployment(we.Type, obj), true
	case ResourceReplicaSet:
		obj, ok := we.Object.(*appsv1.ReplicaSet)
		if !ok {
			return Event{}, false
		}
		return fromReplicaSet(we.Type, obj), true
	case ResourceStatefulSet:
		obj, ok := we.Object.(*appsv1.StatefulSet)
		if !ok {
			return Event{}, false
		}
		return fromStatefulSet(we.Type, obj), true
	case ResourceJob:
		obj, ok := we.Object.(*batchv1.Job)
		if !ok {
			return Event{}, false
		}
		return fromJob(we.Type, obj), true
	case ResourceCronJob:
		obj, ok := we.Object.(*batchv1.CronJob)
		if !ok {
			return Event{}, false
		}
		return fromCronJob(we.Type, obj), true
	case ResourcePVC:
		obj, ok := we.Object.(*corev1.PersistentVolumeClaim)
		if !ok {
			return Event{}, false
		}
		return fromPVC(we.Type, obj), true
	case ResourceConfigMap:
		obj, ok := we.Object.(*corev1.ConfigMap)
		if !ok {
			return Event{}, false
		}
		return fromConfigMap(we.Type, obj), true
	case ResourceSecret:
		obj, ok := we.Object.(*corev1.Secret)
		if !ok {
			return Event{}, false
		}
		return fromSecret(we.Type, obj), true
	case ResourceService:
		obj, ok := we.Object.(*corev1.Service)
		if !ok {
			return Event{}, false
		}
		return fromService(we.Type, obj), true
	case ResourceIngress:
		obj, ok := we.Object.(*networkingv1.Ingress)
		if !ok {
			return Event{}, false
		}
		return fromIngress(we.Type, obj), true
	case ResourceHPA:
		obj, ok := we.Object.(*autoscalingv2.HorizontalPodAutoscaler)
		if !ok {
			return Event{}, false
		}
		return fromHPA(we.Type, obj), true
	case ResourceResourceQuota:
		obj, ok := we.Object.(*corev1.ResourceQuota)
		if !ok {
			return Event{}, false
		}
		return fromResourceQuota(we.Type, obj), true
	case ResourceLimitRange:
		obj, ok := we.Object.(*corev1.LimitRange)
		if !ok {
			return Event{}, false
		}
		return fromLimitRange(we.Type, obj), true
	default:
		return Event{}, false
	}
}

func fromPod(t watch.EventType, pod *corev1.Pod) Event {
	return Event{
		Type:            t,
		Resource:        ResourcePod,
		Namespace:       pod.Namespace,
		Name:            pod.Name,
		ResourceVersion: pod.ResourceVersion,
		PodPhase:        string(pod.Status.Phase),
		At:              time.Now().UTC(),
	}
}

func fromEvent(t watch.EventType, ev *corev1.Event) Event {
	return Event{
		Type:            t,
		Resource:        ResourceEvent,
		Namespace:       ev.Namespace,
		Name:            ev.Name,
		ResourceVersion: ev.ResourceVersion,
		Reason:          ev.Reason,
		Message:         ev.Message,
		InvolvedKind:    ev.InvolvedObject.Kind,
		InvolvedName:    ev.InvolvedObject.Name,
		At:              time.Now().UTC(),
	}
}

func fromDeployment(t watch.EventType, d *appsv1.Deployment) Event {
	desired := int32(1)
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	return Event{
		Type:            t,
		Resource:        ResourceDeployment,
		Namespace:       d.Namespace,
		Name:            d.Name,
		ResourceVersion: d.ResourceVersion,
		Detail:          fmt.Sprintf("ready %d/%d updated=%d available=%d", d.Status.ReadyReplicas, desired, d.Status.UpdatedReplicas, d.Status.AvailableReplicas),
		At:              time.Now().UTC(),
	}
}

func fromReplicaSet(t watch.EventType, rs *appsv1.ReplicaSet) Event {
	desired := int32(0)
	if rs.Spec.Replicas != nil {
		desired = *rs.Spec.Replicas
	}
	return Event{
		Type:            t,
		Resource:        ResourceReplicaSet,
		Namespace:       rs.Namespace,
		Name:            rs.Name,
		ResourceVersion: rs.ResourceVersion,
		Detail:          fmt.Sprintf("ready %d/%d", rs.Status.ReadyReplicas, desired),
		At:              time.Now().UTC(),
	}
}

func fromStatefulSet(t watch.EventType, s *appsv1.StatefulSet) Event {
	desired := int32(1)
	if s.Spec.Replicas != nil {
		desired = *s.Spec.Replicas
	}
	return Event{
		Type:            t,
		Resource:        ResourceStatefulSet,
		Namespace:       s.Namespace,
		Name:            s.Name,
		ResourceVersion: s.ResourceVersion,
		Detail:          fmt.Sprintf("ready %d/%d current=%d", s.Status.ReadyReplicas, desired, s.Status.CurrentReplicas),
		At:              time.Now().UTC(),
	}
}

func fromJob(t watch.EventType, j *batchv1.Job) Event {
	phase := "Running"
	for _, c := range j.Status.Conditions {
		if c.Status != corev1.ConditionTrue {
			continue
		}
		switch c.Type {
		case batchv1.JobComplete:
			phase = "Complete"
		case batchv1.JobFailed:
			phase = "Failed"
		}
	}
	return Event{
		Type:            t,
		Resource:        ResourceJob,
		Namespace:       j.Namespace,
		Name:            j.Name,
		ResourceVersion: j.ResourceVersion,
		PodPhase:        phase,
		Detail:          fmt.Sprintf("active=%d succeeded=%d failed=%d", j.Status.Active, j.Status.Succeeded, j.Status.Failed),
		At:              time.Now().UTC(),
	}
}

func fromCronJob(t watch.EventType, c *batchv1.CronJob) Event {
	suspended := false
	if c.Spec.Suspend != nil {
		suspended = *c.Spec.Suspend
	}
	return Event{
		Type:            t,
		Resource:        ResourceCronJob,
		Namespace:       c.Namespace,
		Name:            c.Name,
		ResourceVersion: c.ResourceVersion,
		Detail:          fmt.Sprintf("schedule=%q active=%d suspended=%t", c.Spec.Schedule, len(c.Status.Active), suspended),
		At:              time.Now().UTC(),
	}
}

func fromPVC(t watch.EventType, p *corev1.PersistentVolumeClaim) Event {
	return Event{
		Type:            t,
		Resource:        ResourcePVC,
		Namespace:       p.Namespace,
		Name:            p.Name,
		ResourceVersion: p.ResourceVersion,
		PodPhase:        string(p.Status.Phase),
		At:              time.Now().UTC(),
	}
}

func fromConfigMap(t watch.EventType, cm *corev1.ConfigMap) Event {
	return Event{
		Type:            t,
		Resource:        ResourceConfigMap,
		Namespace:       cm.Namespace,
		Name:            cm.Name,
		ResourceVersion: cm.ResourceVersion,
		Detail:          fmt.Sprintf("keys=%d", len(cm.Data)+len(cm.BinaryData)),
		At:              time.Now().UTC(),
	}
}

// fromSecret reports only metadata + key count — never Secret values (ADR-0013).
func fromSecret(t watch.EventType, s *corev1.Secret) Event {
	return Event{
		Type:            t,
		Resource:        ResourceSecret,
		Namespace:       s.Namespace,
		Name:            s.Name,
		ResourceVersion: s.ResourceVersion,
		Detail:          fmt.Sprintf("type=%s keys=%d", s.Type, len(s.Data)),
		At:              time.Now().UTC(),
	}
}

func sleepBackoff(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func nextBackoff(cur, max time.Duration) time.Duration {
	n := cur * 2
	if n > max {
		return max
	}
	return n
}

// Object returns the underlying runtime.Object when present (for tests / future context).
func Object(we watch.Event) runtime.Object { return we.Object }
