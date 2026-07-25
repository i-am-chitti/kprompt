package watch

import (
	"context"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestNormalizePodAndEvent(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "api-1", Namespace: "payments", ResourceVersion: "10"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
	ev := fromPod(watch.Modified, pod)
	if ev.Resource != ResourcePod || ev.Name != "api-1" || ev.PodPhase != "Running" {
		t.Fatalf("pod event: %+v", ev)
	}

	kev := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{Name: "api-1.123", Namespace: "payments", ResourceVersion: "11"},
		Reason:     "BackOff",
		Message:    "Back-off restarting failed container",
		InvolvedObject: corev1.ObjectReference{
			Kind: "Pod",
			Name: "api-1",
		},
	}
	got := fromEvent(watch.Added, kev)
	if got.Reason != "BackOff" || got.InvolvedName != "api-1" {
		t.Fatalf("k8s event: %+v", got)
	}
}

func TestEngineSeesPodCreate(t *testing.T) {
	client := fake.NewSimpleClientset()
	watcher := watch.NewFake()
	client.PrependWatchReactor("pods", func(action k8stesting.Action) (bool, watch.Interface, error) {
		return true, watcher, nil
	})

	var mu sync.Mutex
	var got []Event
	eng := &Engine{
		Client: client,
		Options: Options{
			Namespace:   "payments",
			Resources:   []string{ResourcePod},
			MinBackoff:  10 * time.Millisecond,
			MaxBackoff:  50 * time.Millisecond,
			EmitInitial: false,
		},
		Handler: func(ev Event) {
			mu.Lock()
			got = append(got, ev)
			mu.Unlock()
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- eng.Run(ctx) }()

	// Allow list+watch to start.
	time.Sleep(50 * time.Millisecond)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "payments", ResourceVersion: "5"},
		Status:     corev1.PodStatus{Phase: corev1.PodPending},
	}
	watcher.Add(pod)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-errCh

	mu.Lock()
	defer mu.Unlock()
	if len(got) == 0 {
		t.Fatal("expected at least one pod event")
	}
	if got[0].Name != "demo" || got[0].Resource != ResourcePod {
		t.Fatalf("unexpected event: %+v", got[0])
	}
}

func TestEngineEmitInitial(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "existing", Namespace: "ns", ResourceVersion: "1"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
	client := fake.NewSimpleClientset(pod)
	// Block real watches from hanging forever after list.
	client.PrependWatchReactor("pods", func(action k8stesting.Action) (bool, watch.Interface, error) {
		return true, watch.NewFake(), nil
	})

	var mu sync.Mutex
	var got []Event
	eng := &Engine{
		Client: client,
		Options: Options{
			Namespace:   "ns",
			Resources:   []string{ResourcePod},
			EmitInitial: true,
			MinBackoff:  10 * time.Millisecond,
			MaxBackoff:  20 * time.Millisecond,
		},
		Handler: func(ev Event) {
			mu.Lock()
			got = append(got, ev)
			mu.Unlock()
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_ = eng.Run(ctx)

	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, ev := range got {
		if ev.Name == "existing" && ev.Type == watch.Added {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected initial Added for existing pod, got %#v", got)
	}
}

func TestNextBackoff(t *testing.T) {
	if nextBackoff(time.Second, 30*time.Second) != 2*time.Second {
		t.Fatal("double")
	}
	if nextBackoff(20*time.Second, 30*time.Second) != 30*time.Second {
		t.Fatal("cap")
	}
}

// Ensure fake list works for events resource type registration.
var _ runtime.Object = &corev1.Event{}
