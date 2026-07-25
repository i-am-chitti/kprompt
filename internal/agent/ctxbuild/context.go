// Package ctxbuild assembles a structured LLM context from an Incident (AG-007).
//
// It gathers live cluster facts (pod/deployment status, replicas, limits, recent
// events) and merges incident evidence (including on-demand logs). Missing
// optional signals are listed in Degraded — never invented.
//
// When S-002 investigate lands, prefer sharing its chain helpers; this package
// stays the agent-facing AgentContext contract.
package ctxbuild

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/kprompt/kprompt/internal/incident"
)

const (
	APIVersion    = "kprompt.io/v1"
	Kind          = "AgentContext"
	SchemaVersion = "1"

	defaultEventLimit = 20
	defaultCMLimit    = 10
)

// ContainerResources is a compact requests/limits snapshot.
type ContainerResources struct {
	Name            string `json:"name"`
	Image           string `json:"image,omitempty"`
	CPURequest      string `json:"cpuRequest,omitempty"`
	CPULimit        string `json:"cpuLimit,omitempty"`
	MemoryRequest   string `json:"memoryRequest,omitempty"`
	MemoryLimit     string `json:"memoryLimit,omitempty"`
	RestartCount    int32  `json:"restartCount,omitempty"`
	Ready           bool   `json:"ready,omitempty"`
	State           string `json:"state,omitempty"`
	LastTermination string `json:"lastTermination,omitempty"`
}

// PodSnapshot is live pod status for the analyzer.
type PodSnapshot struct {
	Name       string               `json:"name"`
	Namespace  string               `json:"namespace"`
	Phase      string               `json:"phase"`
	Node       string               `json:"node,omitempty"`
	Containers []ContainerResources `json:"containers,omitempty"`
}

// DeploymentSnapshot is replica / rollout status.
type DeploymentSnapshot struct {
	Name               string `json:"name"`
	Namespace          string `json:"namespace"`
	DesiredReplicas    int32  `json:"desiredReplicas"`
	ReadyReplicas      int32  `json:"readyReplicas"`
	UpdatedReplicas    int32  `json:"updatedReplicas"`
	AvailableReplicas  int32  `json:"availableReplicas"`
	Generation         int64  `json:"generation,omitempty"`
	ObservedGeneration int64  `json:"observedGeneration,omitempty"`
	ChangeCause        string `json:"changeCause,omitempty"`
}

// ConfigMapTouch is a recently seen ConfigMap (name + age only — no data).
type ConfigMapTouch struct {
	Name string `json:"name"`
	Age  string `json:"age,omitempty"`
}

// AgentContext is the structured payload for AG-008 LLM analysis.
type AgentContext struct {
	APIVersion     string                 `json:"apiVersion"`
	Kind           string                 `json:"kind"`
	SchemaVersion  string                 `json:"schemaVersion"`
	Namespace      string                 `json:"namespace"`
	BuiltAt        time.Time              `json:"builtAt"`
	Incident       incident.Incident      `json:"incident"`
	Target         *incident.ResourceRef  `json:"target,omitempty"`
	Pod            *PodSnapshot           `json:"pod,omitempty"`
	Deployment     *DeploymentSnapshot    `json:"deployment,omitempty"`
	RecentEvents   []incident.EvidenceRef `json:"recentEvents,omitempty"`
	LogSnippets    []incident.EvidenceRef `json:"logSnippets,omitempty"`
	ConfigMaps     []ConfigMapTouch       `json:"configMaps,omitempty"`
	PriorIncidents []incident.Incident    `json:"priorIncidents,omitempty"`
	Degraded       []string               `json:"degraded,omitempty"`
}

// Options configures context assembly.
type Options struct {
	PriorIncidents []incident.Incident
	EventLimit     int
	SkipLive       bool // tests / offline — only reshape incident evidence
}

// Builder reads the cluster to enrich an Incident.
type Builder struct {
	Client kubernetes.Interface
}

// Build returns an AgentContext for the analyzer.
func (b *Builder) Build(ctx context.Context, inc incident.Incident, opts Options) AgentContext {
	now := time.Now().UTC()
	out := AgentContext{
		APIVersion:     APIVersion,
		Kind:           Kind,
		SchemaVersion:  SchemaVersion,
		Namespace:      firstNonEmpty(inc.Namespace, "default"),
		BuiltAt:        now,
		Incident:       inc,
		Target:         inc.PrimaryResource,
		PriorIncidents: append([]incident.Incident(nil), opts.PriorIncidents...),
	}

	out.LogSnippets = filterEvidence(inc.Evidence, incident.EvidenceLog)
	out.RecentEvents = filterEvidence(inc.Evidence, incident.EvidenceEvent)

	if opts.SkipLive || b == nil || b.Client == nil {
		if b == nil || b.Client == nil {
			out.Degraded = append(out.Degraded, "kubernetes")
		}
		return out
	}

	ns := out.Namespace
	workload, kind := resolveWorkload(inc)

	if !opts.SkipLive {
		b.enrichDeployment(ctx, &out, ns, workload, kind)
		b.enrichPod(ctx, &out, ns, workload, kind, inc)
		b.enrichEvents(ctx, &out, ns, workload, opts.EventLimit)
		b.enrichConfigMaps(ctx, &out, ns)
	}
	return out
}

// PromptBlocks returns compact text sections suitable for an LLM system/user prompt.
func (c AgentContext) PromptBlocks() []string {
	var blocks []string
	blocks = append(blocks, fmt.Sprintf("namespace: %s", c.Namespace))
	if c.Target != nil {
		blocks = append(blocks, fmt.Sprintf("target: %s/%s", c.Target.Kind, c.Target.Name))
	}
	blocks = append(blocks, fmt.Sprintf("incident: id=%s severity=%s status=%s summary=%s",
		c.Incident.ID, c.Incident.Severity, c.Incident.Status, c.Incident.Summary))
	if c.Deployment != nil {
		d := c.Deployment
		blocks = append(blocks, fmt.Sprintf("deployment: %s ready=%d/%d updated=%d gen=%d/%d changeCause=%q",
			d.Name, d.ReadyReplicas, d.DesiredReplicas, d.UpdatedReplicas, d.ObservedGeneration, d.Generation, d.ChangeCause))
	}
	if c.Pod != nil {
		p := c.Pod
		blocks = append(blocks, fmt.Sprintf("pod: %s phase=%s node=%s", p.Name, p.Phase, p.Node))
		for _, ctn := range p.Containers {
			blocks = append(blocks, fmt.Sprintf("  container %s image=%s restarts=%d ready=%v state=%s cpu=%s/%s mem=%s/%s lastTerm=%s",
				ctn.Name, ctn.Image, ctn.RestartCount, ctn.Ready, ctn.State,
				ctn.CPURequest, ctn.CPULimit, ctn.MemoryRequest, ctn.MemoryLimit, ctn.LastTermination))
		}
	}
	if len(c.RecentEvents) > 0 {
		blocks = append(blocks, "events:")
		for _, e := range c.RecentEvents {
			blocks = append(blocks, fmt.Sprintf("  - %s: %s", e.Reason, truncate(e.Message, 200)))
		}
	}
	if len(c.LogSnippets) > 0 {
		blocks = append(blocks, "logs:")
		for _, l := range c.LogSnippets {
			blocks = append(blocks, truncate(l.Message, 1500))
		}
	}
	if len(c.ConfigMaps) > 0 {
		var names []string
		for _, cm := range c.ConfigMaps {
			names = append(names, cm.Name)
		}
		blocks = append(blocks, "configmaps: "+strings.Join(names, ", "))
	}
	if len(c.Degraded) > 0 {
		blocks = append(blocks, "degraded: "+strings.Join(c.Degraded, ", "))
	}
	return blocks
}

func (b *Builder) enrichDeployment(ctx context.Context, out *AgentContext, ns, workload, kind string) {
	name := workload
	if kind == "Pod" {
		// try Deployment with same workload basename
	}
	dep, err := b.Client.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return
	}
	if err != nil {
		out.Degraded = appendUnique(out.Degraded, "deployment")
		return
	}
	out.Deployment = deploymentSnap(dep)
	if out.Target == nil {
		out.Target = &incident.ResourceRef{Kind: "Deployment", Name: dep.Name, Namespace: ns}
	}
}

func (b *Builder) enrichPod(ctx context.Context, out *AgentContext, ns, workload, kind string, inc incident.Incident) {
	podName := ""
	if kind == "Pod" {
		// Prefer concrete pod from evidence
		podName = concretePodName(inc)
		if podName == "" {
			podName = workload
		}
	} else {
		podName = concretePodName(inc)
	}

	var pod *corev1.Pod
	var err error
	if podName != "" {
		pod, err = b.Client.CoreV1().Pods(ns).Get(ctx, podName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			pod = nil
			err = nil
		}
	}
	if pod == nil && out.Deployment != nil {
		pod, err = pickDeploymentPod(ctx, b.Client, ns, out.Deployment.Name)
	}
	if err != nil {
		out.Degraded = appendUnique(out.Degraded, "pod")
		return
	}
	if pod == nil {
		out.Degraded = appendUnique(out.Degraded, "pod")
		return
	}
	out.Pod = podSnap(pod)
}

func (b *Builder) enrichEvents(ctx context.Context, out *AgentContext, ns, workload string, limit int) {
	if limit <= 0 {
		limit = defaultEventLimit
	}
	list, err := b.Client.CoreV1().Events(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		out.Degraded = appendUnique(out.Degraded, "events")
		return
	}
	items := list.Items
	sort.Slice(items, func(i, j int) bool {
		return eventTime(items[i]).After(eventTime(items[j]))
	})
	var added int
	for _, ev := range items {
		if workload != "" && ev.InvolvedObject.Name != "" {
			if !strings.HasPrefix(ev.InvolvedObject.Name, workload) && ev.InvolvedObject.Name != workload {
				continue
			}
		}
		ts := eventTime(ev)
		out.RecentEvents = append(out.RecentEvents, incident.EvidenceRef{
			Type: incident.EvidenceEvent,
			Resource: &incident.ResourceRef{
				Kind:      ev.InvolvedObject.Kind,
				Name:      ev.InvolvedObject.Name,
				Namespace: ns,
			},
			Reason:    ev.Reason,
			Message:   ev.Message,
			Timestamp: &ts,
			Source:    "kubernetes",
		})
		added++
		if added >= limit {
			break
		}
	}
}

func (b *Builder) enrichConfigMaps(ctx context.Context, out *AgentContext, ns string) {
	list, err := b.Client.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{Limit: int64(defaultCMLimit)})
	if err != nil {
		out.Degraded = appendUnique(out.Degraded, "configmaps")
		return
	}
	now := time.Now()
	for _, cm := range list.Items {
		age := ""
		if !cm.CreationTimestamp.IsZero() {
			age = formatAge(now.Sub(cm.CreationTimestamp.Time))
		}
		out.ConfigMaps = append(out.ConfigMaps, ConfigMapTouch{Name: cm.Name, Age: age})
	}
}

func resolveWorkload(inc incident.Incident) (name, kind string) {
	if inc.PrimaryResource != nil {
		return inc.PrimaryResource.Name, inc.PrimaryResource.Kind
	}
	for _, a := range inc.Affected {
		return a.Name, a.Kind
	}
	return "", ""
}

func concretePodName(inc incident.Incident) string {
	for _, e := range inc.Evidence {
		if e.Resource != nil && e.Resource.Kind == "Pod" && strings.Count(e.Resource.Name, "-") >= 2 {
			// likely full pod name with hashes
			return e.Resource.Name
		}
	}
	for _, e := range inc.Evidence {
		if e.Resource != nil && e.Resource.Kind == "Pod" {
			return e.Resource.Name
		}
	}
	return ""
}

func deploymentSnap(dep *appsv1.Deployment) *DeploymentSnapshot {
	var desired int32 = 1
	if dep.Spec.Replicas != nil {
		desired = *dep.Spec.Replicas
	}
	cause := dep.Annotations["kubernetes.io/change-cause"]
	return &DeploymentSnapshot{
		Name:               dep.Name,
		Namespace:          dep.Namespace,
		DesiredReplicas:    desired,
		ReadyReplicas:      dep.Status.ReadyReplicas,
		UpdatedReplicas:    dep.Status.UpdatedReplicas,
		AvailableReplicas:  dep.Status.AvailableReplicas,
		Generation:         dep.Generation,
		ObservedGeneration: dep.Status.ObservedGeneration,
		ChangeCause:        cause,
	}
}

func podSnap(pod *corev1.Pod) *PodSnapshot {
	snap := &PodSnapshot{
		Name:      pod.Name,
		Namespace: pod.Namespace,
		Phase:     string(pod.Status.Phase),
		Node:      pod.Spec.NodeName,
	}
	byName := map[string]corev1.Container{}
	for _, c := range pod.Spec.Containers {
		byName[c.Name] = c
	}
	for _, st := range pod.Status.ContainerStatuses {
		cr := ContainerResources{
			Name:         st.Name,
			RestartCount: st.RestartCount,
			Ready:        st.Ready,
			State:        containerState(st),
		}
		if st.LastTerminationState.Terminated != nil {
			t := st.LastTerminationState.Terminated
			cr.LastTermination = fmt.Sprintf("%s exit=%d", t.Reason, t.ExitCode)
		}
		if c, ok := byName[st.Name]; ok {
			cr.Image = c.Image
			cr.CPURequest = qty(c.Resources.Requests, corev1.ResourceCPU)
			cr.CPULimit = qty(c.Resources.Limits, corev1.ResourceCPU)
			cr.MemoryRequest = qty(c.Resources.Requests, corev1.ResourceMemory)
			cr.MemoryLimit = qty(c.Resources.Limits, corev1.ResourceMemory)
		}
		snap.Containers = append(snap.Containers, cr)
	}
	if len(snap.Containers) == 0 {
		for _, c := range pod.Spec.Containers {
			snap.Containers = append(snap.Containers, ContainerResources{
				Name:          c.Name,
				Image:         c.Image,
				CPURequest:    qty(c.Resources.Requests, corev1.ResourceCPU),
				CPULimit:      qty(c.Resources.Limits, corev1.ResourceCPU),
				MemoryRequest: qty(c.Resources.Requests, corev1.ResourceMemory),
				MemoryLimit:   qty(c.Resources.Limits, corev1.ResourceMemory),
			})
		}
	}
	return snap
}

func pickDeploymentPod(ctx context.Context, client kubernetes.Interface, ns, name string) (*corev1.Pod, error) {
	dep, err := client.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	list, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
		LabelSelector: metav1.FormatLabelSelector(dep.Spec.Selector),
	})
	if err != nil {
		return nil, err
	}
	if len(list.Items) == 0 {
		return nil, fmt.Errorf("no pods for Deployment/%s", name)
	}
	pods := append([]corev1.Pod(nil), list.Items...)
	sort.SliceStable(pods, func(i, j int) bool {
		return podProblemScore(pods[i]) > podProblemScore(pods[j])
	})
	return &pods[0], nil
}

func podProblemScore(p corev1.Pod) int {
	score := 0
	switch p.Status.Phase {
	case corev1.PodFailed:
		score += 100
	case corev1.PodPending:
		score += 50
	case corev1.PodRunning:
		score += 10
	}
	for _, cs := range p.Status.ContainerStatuses {
		score += int(cs.RestartCount)
		if !cs.Ready {
			score += 5
		}
	}
	return score
}

func containerState(st corev1.ContainerStatus) string {
	switch {
	case st.State.Waiting != nil:
		return "waiting:" + st.State.Waiting.Reason
	case st.State.Terminated != nil:
		return fmt.Sprintf("terminated:%s", st.State.Terminated.Reason)
	case st.State.Running != nil:
		return "running"
	default:
		return ""
	}
}

func qty(list corev1.ResourceList, name corev1.ResourceName) string {
	if list == nil {
		return ""
	}
	q, ok := list[name]
	if !ok {
		return ""
	}
	return q.String()
}

func eventTime(ev corev1.Event) time.Time {
	if ev.LastTimestamp.Time.IsZero() && ev.EventTime.Time.IsZero() {
		return ev.CreationTimestamp.Time
	}
	if !ev.LastTimestamp.Time.IsZero() {
		return ev.LastTimestamp.Time
	}
	return ev.EventTime.Time
}

func filterEvidence(in []incident.EvidenceRef, typ string) []incident.EvidenceRef {
	var out []incident.EvidenceRef
	for _, e := range in {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func formatAge(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}
