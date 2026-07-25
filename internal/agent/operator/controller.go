package operator

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	watchtools "k8s.io/client-go/tools/watch"
)

// Controller watches KpromptAgent CRs and reconciles them.
type Controller struct {
	Reconciler *Reconciler
	// Namespace empty → all namespaces.
	Namespace string
	Resync    time.Duration
}

// Run blocks until ctx is cancelled.
func (c *Controller) Run(ctx context.Context) error {
	if c == nil || c.Reconciler == nil {
		return fmt.Errorf("operator: reconciler required")
	}
	resync := c.Resync
	if resync <= 0 {
		resync = 5 * time.Minute
	}

	lw := &cache.ListWatch{
		ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
			return c.Reconciler.Dynamic.Resource(gvr).Namespace(c.Namespace).List(ctx, opts)
		},
		WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
			return c.Reconciler.Dynamic.Resource(gvr).Namespace(c.Namespace).Watch(ctx, opts)
		},
	}

	_, err := watchtools.UntilWithSync(ctx, lw, &unstructured.Unstructured{}, nil,
		func(event watch.Event) (bool, error) {
			switch event.Type {
			case watch.Added, watch.Modified, watch.Deleted:
				u, ok := event.Object.(*unstructured.Unstructured)
				if !ok {
					return false, nil
				}
				cr, err := FromUnstructured(u)
				if err != nil {
					return false, nil
				}
				if err := c.Reconciler.Reconcile(ctx, cr); err != nil {
					// Keep watching; surface via Ready=False on next successful status patch attempts.
					fmt.Printf("reconcile %s/%s: %v\n", cr.Namespace, cr.Name, err)
				}
			}
			return false, nil
		},
	)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// ReconcileAll lists and reconciles every KpromptAgent once (startup / --once).
func (c *Controller) ReconcileAll(ctx context.Context) error {
	list, err := c.Reconciler.Dynamic.Resource(gvr).Namespace(c.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for i := range list.Items {
		cr, err := FromUnstructured(&list.Items[i])
		if err != nil {
			continue
		}
		if err := c.Reconciler.Reconcile(ctx, cr); err != nil {
			fmt.Printf("reconcile %s/%s: %v\n", cr.Namespace, cr.Name, err)
		}
	}
	return nil
}
