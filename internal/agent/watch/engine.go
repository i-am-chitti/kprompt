// Package watch implements namespace-scoped Pods + Events watching for the Observe agent (AG-003).
//
// It uses Kubernetes list→watch with reconnect/backoff and optional bookmark events.
// This package is read-only: it never mutates the cluster.
package watch

import (
	"context"
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
)

const (
	ResourcePod   = "Pod"
	ResourceEvent = "Event"

	defaultMinBackoff = 500 * time.Millisecond
	defaultMaxBackoff = 30 * time.Second
)

// Event is a normalized watch notification for agent pipelines.
type Event struct {
	Type            watch.EventType `json:"type"`
	Resource        string          `json:"resource"` // Pod | Event
	Namespace       string          `json:"namespace"`
	Name            string          `json:"name"`
	ResourceVersion string          `json:"resourceVersion,omitempty"`
	Reason          string          `json:"reason,omitempty"`      // Event.reason or Pod condition
	Message         string          `json:"message,omitempty"`     // Event.message
	InvolvedKind    string          `json:"involvedKind,omitempty"`
	InvolvedName    string          `json:"involvedName,omitempty"`
	PodPhase        string          `json:"podPhase,omitempty"`
	At              time.Time       `json:"at"`
}

// Handler receives normalized events. Must be safe for concurrent calls from pod/event loops.
type Handler func(Event)

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
	resources := e.Options.Resources
	if len(resources) == 0 {
		resources = []string{ResourcePod, ResourceEvent}
	}
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
			switch res {
			case ResourcePod:
				loopErr = e.loop(ctx, ResourcePod, minB, maxB, e.listWatchPods)
			case ResourceEvent:
				loopErr = e.loop(ctx, ResourceEvent, minB, maxB, e.listWatchEvents)
			default:
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

func normalize(resource string, we watch.Event) (Event, bool) {
	switch resource {
	case ResourcePod:
		pod, ok := we.Object.(*corev1.Pod)
		if !ok {
			return Event{}, false
		}
		return fromPod(we.Type, pod), true
	case ResourceEvent:
		ev, ok := we.Object.(*corev1.Event)
		if !ok {
			return Event{}, false
		}
		return fromEvent(we.Type, ev), true
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
