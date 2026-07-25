// Package health computes a namespace Observe health score (AG-011).
//
// Score is 0–100 from open incidents (severity-weighted) plus optional live
// pod readiness / restart pressure. Works without Prometheus; missing live
// signals are listed in Degraded.
package health

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/kprompt/kprompt/internal/incident"
)

const (
	KindSnapshot  = "HealthSnapshot"
	APIVersion    = "kprompt.io/v1"
	SchemaVersion = "1"

	DefaultDropThreshold = 10
	DefaultHistorySize   = 20
)

// Snapshot is the current namespace health view.
type Snapshot struct {
	APIVersion    string    `json:"apiVersion"`
	Kind          string    `json:"kind"`
	SchemaVersion string    `json:"schemaVersion"`
	Namespace     string    `json:"namespace"`
	Score         int       `json:"score"` // 0..100
	PreviousScore *int      `json:"previousScore,omitempty"`
	Trend         string    `json:"trend"` // stable | improving | risk_increasing
	OpenIncidents int       `json:"openIncidents"`
	PodReady      string    `json:"podReady,omitempty"` // e.g. 8/10
	Restarts      int       `json:"restarts,omitempty"`
	Degraded      []string  `json:"degraded,omitempty"`
	At            time.Time `json:"at"`
	Message       string    `json:"message,omitempty"`
}

// Tracker maintains score history for trend detection.
type Tracker struct {
	Namespace     string
	DropThreshold int
	Client        kubernetes.Interface // optional — pod readiness/restarts
	Now           func() time.Time

	mu      sync.Mutex
	history []int
}

// NewTracker returns a health tracker for one namespace.
func NewTracker(namespace string, client kubernetes.Interface) *Tracker {
	return &Tracker{
		Namespace:     namespace,
		DropThreshold: DefaultDropThreshold,
		Client:        client,
		Now:           func() time.Time { return time.Now().UTC() },
		history:       nil,
	}
}

// Evaluate computes a new snapshot from open incidents (+ optional pod stats).
func (t *Tracker) Evaluate(ctx context.Context, open []incident.Incident) Snapshot {
	now := t.Now()
	if t.Now == nil {
		now = time.Now().UTC()
	}
	ns := t.Namespace
	score := 100
	var degraded []string

	score -= incidentPenalty(open)
	podReady := ""
	restarts := 0
	if t.Client != nil {
		ready, total, rst, err := podStats(ctx, t.Client, ns)
		if err != nil {
			degraded = append(degraded, "pods")
		} else {
			podReady = fmt.Sprintf("%d/%d", ready, total)
			restarts = rst
			score -= podPenalty(ready, total, rst)
		}
	} else {
		degraded = append(degraded, "pods")
	}
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	var prev *int
	trend := "stable"
	msg := fmt.Sprintf("namespace %s health %d/100", ns, score)
	if len(t.history) > 0 {
		p := t.history[len(t.history)-1]
		prev = &p
		drop := p - score
		thresh := t.DropThreshold
		if thresh <= 0 {
			thresh = DefaultDropThreshold
		}
		switch {
		case drop >= thresh:
			trend = "risk_increasing"
			msg = fmt.Sprintf("Risk increasing: health %d → %d (−%d)", p, score, drop)
		case score-p >= thresh:
			trend = "improving"
			msg = fmt.Sprintf("Health improving: %d → %d", p, score)
		}
	}
	t.history = append(t.history, score)
	if len(t.history) > DefaultHistorySize {
		t.history = t.history[len(t.history)-DefaultHistorySize:]
	}

	return Snapshot{
		APIVersion:    APIVersion,
		Kind:          KindSnapshot,
		SchemaVersion: SchemaVersion,
		Namespace:     ns,
		Score:         score,
		PreviousScore: prev,
		Trend:         trend,
		OpenIncidents: len(open),
		PodReady:      podReady,
		Restarts:      restarts,
		Degraded:      degraded,
		At:            now,
		Message:       msg,
	}
}

// LastScore returns the most recent score if any.
func (t *Tracker) LastScore() (int, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.history) == 0 {
		return 0, false
	}
	return t.history[len(t.history)-1], true
}

func incidentPenalty(open []incident.Incident) int {
	pen := 0
	for _, inc := range open {
		switch strings.ToLower(inc.Severity) {
		case incident.SeverityCritical:
			pen += 25
		case incident.SeverityHigh:
			pen += 15
		case incident.SeverityMedium:
			pen += 8
		case incident.SeverityLow:
			pen += 4
		case incident.SeverityInfo:
			pen += 2
		default:
			pen += 8
		}
	}
	if pen > 80 {
		return 80
	}
	return pen
}

func podPenalty(ready, total, restarts int) int {
	if total <= 0 {
		return 0
	}
	notReady := total - ready
	pen := notReady * 5
	if restarts > 10 {
		pen += 15
	} else if restarts > 5 {
		pen += 8
	} else if restarts > 0 {
		pen += 3
	}
	if pen > 30 {
		return 30
	}
	return pen
}

func podStats(ctx context.Context, client kubernetes.Interface, ns string) (ready, total, restarts int, err error) {
	list, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, 0, 0, err
	}
	total = len(list.Items)
	for _, p := range list.Items {
		if podReady(p) {
			ready++
		}
		for _, cs := range p.Status.ContainerStatuses {
			restarts += int(cs.RestartCount)
		}
	}
	return ready, total, restarts, nil
}

func podReady(p corev1.Pod) bool {
	if p.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
