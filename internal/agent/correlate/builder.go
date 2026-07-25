// Package correlate groups watch events into Incidents (AG-006).
//
// Rules (in-memory; durable store is later):
//
//  1. Key: workload identity — Pod names strip ReplicaSet+Pod hash suffixes;
//     Kubernetes Events use involvedObject (Pod names normalized the same way).
//  2. Open: only "problem" signals open or attach (BackOff, Failed, OOM, Unhealthy,
//     FailedScheduling, CrashLoop-ish reasons; Pod phase Failed/Pending).
//  3. Window: new problem for the same key within Window joins the open Incident.
//  4. Dedupe: identical reason+message+involved fingerprint within DedupeTTL is ignored.
//  5. Close: idle QuietFor with no new evidence, or an explicit recovery signal
//     (Pod phase Running) while open → status resolved then closed.
//  6. Reopen: a problem after close within ReopenWithin creates a new Incident
//     (new id) rather than mutating history silently.
//
// Observe Mode only — never mutates the cluster.
package correlate

import (
	"fmt"
	"strings"
	"sync"
	"time"

	agentwatch "github.com/kprompt/kprompt/internal/agent/watch"
	"github.com/kprompt/kprompt/internal/incident"
)

const (
	ChangeOpened   = "opened"
	ChangeUpdated  = "updated"
	ChangeClosed   = "closed"
	ChangeReopened = "reopened"
	ChangeIgnored  = "ignored"

	DefaultWindow       = 5 * time.Minute
	DefaultQuietFor     = 10 * time.Minute
	DefaultDedupeTTL    = 1 * time.Minute
	DefaultReopenWithin = 30 * time.Minute
)

// Change is emitted when the incident set mutates.
type Change struct {
	Kind     string            `json:"kind"`
	Incident incident.Incident `json:"incident"`
}

// Options configures correlation behaviour.
type Options struct {
	Namespace    string
	Window       time.Duration
	QuietFor     time.Duration
	DedupeTTL    time.Duration
	ReopenWithin time.Duration
	Now          func() time.Time
}

// Builder holds open incidents for one namespace.
type Builder struct {
	opts   Options
	mu     sync.Mutex
	open   map[string]*incident.Incident // workload key → open incident
	recent map[string]closedRef          // workload key → last closed (for reopen window)
	dedupe map[string]time.Time          // fingerprint → last seen
	seq    int
}

type closedRef struct {
	at time.Time
	id string
}

// NewBuilder returns an in-memory incident correlator.
func NewBuilder(opts Options) *Builder {
	if opts.Window <= 0 {
		opts.Window = DefaultWindow
	}
	if opts.QuietFor <= 0 {
		opts.QuietFor = DefaultQuietFor
	}
	if opts.DedupeTTL <= 0 {
		opts.DedupeTTL = DefaultDedupeTTL
	}
	if opts.ReopenWithin <= 0 {
		opts.ReopenWithin = DefaultReopenWithin
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Builder{
		opts:   opts,
		open:   map[string]*incident.Incident{},
		recent: map[string]closedRef{},
		dedupe: map[string]time.Time{},
	}
}

// Ingest folds a watch event into the incident set.
func (b *Builder) Ingest(ev agentwatch.Event) (Change, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.opts.Now()
	if ev.At.IsZero() {
		ev.At = now
	}
	key := workloadKey(ev)
	if key == "" {
		return Change{Kind: ChangeIgnored}, false
	}

	problem := isProblem(ev)
	recovery := isRecovery(ev)
	if !problem && !recovery {
		return Change{Kind: ChangeIgnored}, false
	}

	if problem {
		fp := fingerprint(key, ev)
		if last, ok := b.dedupe[fp]; ok && now.Sub(last) < b.opts.DedupeTTL {
			return Change{Kind: ChangeIgnored}, false
		}
		b.dedupe[fp] = now
	}

	if cur, ok := b.open[key]; ok {
		if problem {
			if now.Sub(cur.UpdatedAt) > b.opts.Window && now.Sub(cur.StartedAt) > b.opts.Window {
				// Outside correlation window: close old, open new.
				b.closeLocked(key, cur, now, incident.StatusClosed)
				return b.openNewLocked(key, ev, now, ChangeOpened), true
			}
			attachEvidence(cur, ev, now)
			bumpSummary(cur, ev)
			return Change{Kind: ChangeUpdated, Incident: cloneIncident(cur)}, true
		}
		// recovery on open incident
		attachEvidence(cur, ev, now)
		cur.Status = incident.StatusResolved
		cur.UpdatedAt = now
		cur.Summary = fmt.Sprintf("Recovered: %s", cur.Summary)
		closed := b.closeLocked(key, cur, now, incident.StatusClosed)
		return Change{Kind: ChangeClosed, Incident: closed}, true
	}

	if !problem {
		return Change{Kind: ChangeIgnored}, false
	}

	kind := ChangeOpened
	if ref, ok := b.recent[key]; ok && now.Sub(ref.at) <= b.opts.ReopenWithin {
		kind = ChangeReopened
	}
	return b.openNewLocked(key, ev, now, kind), true
}

// Sweep closes open incidents that have been quiet longer than QuietFor.
func (b *Builder) Sweep() []Change {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.opts.Now()
	var out []Change
	for key, inc := range b.open {
		if now.Sub(inc.UpdatedAt) < b.opts.QuietFor {
			continue
		}
		closed := b.closeLocked(key, inc, now, incident.StatusClosed)
		out = append(out, Change{Kind: ChangeClosed, Incident: closed})
	}
	b.pruneDedupeLocked(now)
	return out
}

// OpenIncidents returns a snapshot of currently open incidents.
func (b *Builder) OpenIncidents() []incident.Incident {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]incident.Incident, 0, len(b.open))
	for _, inc := range b.open {
		out = append(out, cloneIncident(inc))
	}
	return out
}

func (b *Builder) openNewLocked(key string, ev agentwatch.Event, now time.Time, kind string) Change {
	b.seq++
	id := fmt.Sprintf("inc-%d", b.seq)
	inc := incident.NewIncident(id, b.opts.Namespace, now)
	if inc.Namespace == "" {
		inc.Namespace = ev.Namespace
	}
	inc.Severity = severityFor(ev)
	inc.PrimaryResource = primaryRef(ev)
	attachEvidence(&inc, ev, now)
	bumpSummary(&inc, ev)
	b.open[key] = &inc
	return Change{Kind: kind, Incident: cloneIncident(&inc)}
}

func (b *Builder) closeLocked(key string, cur *incident.Incident, now time.Time, status string) incident.Incident {
	cur.Status = status
	cur.UpdatedAt = now
	t := now
	cur.ClosedAt = &t
	snap := cloneIncident(cur)
	delete(b.open, key)
	b.recent[key] = closedRef{at: now, id: cur.ID}
	return snap
}

func (b *Builder) pruneDedupeLocked(now time.Time) {
	for fp, at := range b.dedupe {
		if now.Sub(at) > b.opts.DedupeTTL*2 {
			delete(b.dedupe, fp)
		}
	}
}

func workloadKey(ev agentwatch.Event) string {
	switch ev.Resource {
	case agentwatch.ResourceEvent:
		kind := ev.InvolvedKind
		name := ev.InvolvedName
		if kind == "" {
			kind = "Event"
		}
		if name == "" {
			name = ev.Name
		}
		if kind == "Pod" {
			name = podWorkloadName(name)
		}
		return kind + "/" + name
	case agentwatch.ResourcePod:
		return "Pod/" + podWorkloadName(ev.Name)
	default:
		if ev.Name == "" {
			return ""
		}
		return ev.Resource + "/" + ev.Name
	}
}

// podWorkloadName strips ReplicaSet and Pod template hash suffixes when present.
func podWorkloadName(pod string) string {
	parts := strings.Split(pod, "-")
	if len(parts) >= 3 {
		last := parts[len(parts)-1]
		second := parts[len(parts)-2]
		if looksLikeHash(last) && looksLikeHash(second) {
			return strings.Join(parts[:len(parts)-2], "-")
		}
	}
	return pod
}

func looksLikeHash(s string) bool {
	if n := len(s); n < 5 || n > 10 {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func isProblem(ev agentwatch.Event) bool {
	if ev.Resource == agentwatch.ResourcePod {
		switch ev.PodPhase {
		case "Failed", "Pending", "Unknown":
			return true
		default:
			return false
		}
	}
	reason := strings.ToLower(strings.TrimSpace(ev.Reason))
	switch reason {
	case "backoff", "failed", "unhealthy", "failedscheduling", "oomkilling", "oomkilled",
		"evicted", "crashloopbackoff", "failedmount", "failedattachvolume", "probeerror",
		"failedcreatedpodcontainer", "networknotready", "failedkillpod":
		return true
	}
	msg := strings.ToLower(ev.Message)
	if strings.Contains(msg, "crashloop") || strings.Contains(msg, "oomkilled") {
		return true
	}
	return false
}

func isRecovery(ev agentwatch.Event) bool {
	if ev.Resource == agentwatch.ResourcePod && ev.PodPhase == "Running" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(ev.Reason)) {
	case "started", "healthy", "pulled":
		// "Pulled" alone is weak; only treat Started/Healthy as recovery.
		return strings.EqualFold(ev.Reason, "Started") || strings.EqualFold(ev.Reason, "Healthy")
	default:
		return false
	}
}

func severityFor(ev agentwatch.Event) string {
	r := strings.ToLower(ev.Reason)
	switch {
	case strings.Contains(r, "oom"), strings.Contains(strings.ToLower(ev.Message), "oom"):
		return incident.SeverityCritical
	case strings.Contains(r, "backoff"), strings.Contains(r, "crash"), ev.PodPhase == "Failed":
		return incident.SeverityHigh
	case ev.PodPhase == "Pending", r == "failedscheduling":
		return incident.SeverityMedium
	default:
		return incident.SeverityMedium
	}
}

func primaryRef(ev agentwatch.Event) *incident.ResourceRef {
	if ev.Resource == agentwatch.ResourceEvent && ev.InvolvedName != "" {
		kind := ev.InvolvedKind
		if kind == "" {
			kind = "Pod"
		}
		name := ev.InvolvedName
		if kind == "Pod" {
			name = podWorkloadName(name)
		}
		return &incident.ResourceRef{Kind: kind, Name: name, Namespace: ev.Namespace}
	}
	if ev.Resource == agentwatch.ResourcePod {
		return &incident.ResourceRef{Kind: "Pod", Name: podWorkloadName(ev.Name), Namespace: ev.Namespace}
	}
	return &incident.ResourceRef{Kind: ev.Resource, Name: ev.Name, Namespace: ev.Namespace}
}

func attachEvidence(inc *incident.Incident, ev agentwatch.Event, now time.Time) {
	ts := ev.At
	if ts.IsZero() {
		ts = now
	}
	ref := primaryRef(ev)
	evType := incident.EvidenceEvent
	if ev.Resource == agentwatch.ResourcePod {
		evType = incident.EvidenceObject
	}
	inc.Evidence = append(inc.Evidence, incident.EvidenceRef{
		Type:      evType,
		Resource:  ref,
		Reason:    firstNonEmpty(ev.Reason, ev.PodPhase),
		Message:   ev.Message,
		Timestamp: &ts,
		Source:    "kubernetes",
	})
	if ref != nil {
		addAffected(inc, *ref)
	}
	inc.UpdatedAt = now
	if sev := severityFor(ev); severityRank(sev) > severityRank(inc.Severity) {
		inc.Severity = sev
	}
}

func bumpSummary(inc *incident.Incident, ev agentwatch.Event) {
	label := firstNonEmpty(ev.Reason, ev.PodPhase, string(ev.Type))
	target := ""
	if inc.PrimaryResource != nil {
		target = inc.PrimaryResource.Kind + "/" + inc.PrimaryResource.Name
	}
	inc.Summary = fmt.Sprintf("%s on %s (%d signals)", label, target, len(inc.Evidence))
}

func addAffected(inc *incident.Incident, ref incident.ResourceRef) {
	for _, a := range inc.Affected {
		if a.Kind == ref.Kind && a.Name == ref.Name && a.Namespace == ref.Namespace {
			return
		}
	}
	inc.Affected = append(inc.Affected, ref)
}

func fingerprint(key string, ev agentwatch.Event) string {
	return strings.Join([]string{
		key,
		ev.Resource,
		ev.Reason,
		ev.Message,
		ev.InvolvedName,
		ev.PodPhase,
	}, "|")
}

func cloneIncident(inc *incident.Incident) incident.Incident {
	if inc == nil {
		return incident.Incident{}
	}
	out := *inc
	if inc.ClosedAt != nil {
		t := *inc.ClosedAt
		out.ClosedAt = &t
	}
	if inc.PrimaryResource != nil {
		r := *inc.PrimaryResource
		out.PrimaryResource = &r
	}
	out.Affected = append([]incident.ResourceRef(nil), inc.Affected...)
	out.Evidence = append([]incident.EvidenceRef(nil), inc.Evidence...)
	out.Findings = append([]incident.Finding(nil), inc.Findings...)
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func severityRank(s string) int {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case incident.SeverityInfo:
		return 1
	case incident.SeverityLow:
		return 2
	case incident.SeverityMedium:
		return 3
	case incident.SeverityHigh:
		return 4
	case incident.SeverityCritical:
		return 5
	default:
		return 0
	}
}
