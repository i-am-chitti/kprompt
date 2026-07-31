package graph

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"

	"github.com/kprompt/kprompt/internal/cluster"
)

const (
	ScopeCluster   = "cluster"
	ScopeNamespace = "namespace"

	NodeService       = "Service"
	NodePod           = "Pod"
	NodeNetworkPolicy = "NetworkPolicy"
	NodeIngress       = "Ingress"
	NodePVC           = "PersistentVolumeClaim"
	EdgeRoutes        = "routes"
	EdgeSelects       = "selects"
	EdgeExposes       = "exposes" // Ingress → Service
	EdgeMounts        = "mounts"  // Pod → PVC
	SourceKubernetes  = "k8s"
	// SourceOTel / EdgeCalls are set in otel.go (T-060).
)

// Request configures a read-only service dependency graph.
type Request struct {
	Namespace            string // empty → cluster-wide
	IncludeNetworkPolicy bool
	// IncludeIngress adds Ingress→Service exposes edges (AG-063; default true from callers).
	IncludeIngress bool
	// IncludePVC adds Pod→PVC mounts edges (AG-063; default true from callers).
	IncludePVC bool
}

// Node is one graph vertex.
type Node struct {
	ID        string            `json:"id"`
	Kind      string            `json:"kind"`
	Name      string            `json:"name"`
	Namespace string            `json:"namespace,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// Edge is one directed dependency.
type Edge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Type   string `json:"type"` // routes | selects | calls | allows | denies | exposes | mounts
	Detail string `json:"detail,omitempty"`
	Source string `json:"source"` // k8s | otel
}

// Report is the stable human + JSON contract for service dependency graphs (T-059).
type Report struct {
	Type      string   `json:"type"`
	Scope     string   `json:"scope"`
	Namespace string   `json:"namespace,omitempty"`
	Summary   string   `json:"summary"`
	Nodes     []Node   `json:"nodes"`
	Edges     []Edge   `json:"edges"`
	Notes     []string `json:"notes,omitempty"`
}

// Build collects Services + EndpointSlices (+ optional NetworkPolicies) into a graph.
func Build(ctx context.Context, client kubernetes.Interface, req Request) (Report, error) {
	if client == nil {
		return Report{}, fmt.Errorf("kubernetes client is required for service graph")
	}
	ns := strings.TrimSpace(req.Namespace)
	scope := ScopeCluster
	if ns != "" {
		scope = ScopeNamespace
	}
	rep := Report{
		Type:      "service-graph",
		Scope:     scope,
		Namespace: ns,
		Nodes:     make([]Node, 0, 32),
		Edges:     make([]Edge, 0, 64),
	}
	limit := int64(cluster.DefaultReadLimit)
	nodes := map[string]Node{}
	var notes []string

	svcs, err := client.CoreV1().Services(ns).List(ctx, metav1.ListOptions{Limit: limit})
	if err != nil {
		if apierrors.IsForbidden(err) {
			notes = append(notes, fmt.Sprintf("skipped Services: %v", err))
		} else {
			return Report{}, fmt.Errorf("list services: %w", err)
		}
	} else {
		for _, svc := range svcs.Items {
			id := nodeID(NodeService, svc.Namespace, svc.Name)
			nodes[id] = Node{
				ID: id, Kind: NodeService, Name: svc.Name, Namespace: svc.Namespace,
				Labels: copyLabels(svc.Labels),
			}
		}
	}

	slices, err := client.DiscoveryV1().EndpointSlices(ns).List(ctx, metav1.ListOptions{Limit: limit})
	if err != nil {
		if apierrors.IsForbidden(err) {
			notes = append(notes, fmt.Sprintf("skipped EndpointSlices: %v", err))
		} else {
			return Report{}, fmt.Errorf("list endpointslices: %w", err)
		}
	} else {
		for _, slice := range slices.Items {
			svcName := slice.Labels[discoveryv1.LabelServiceName]
			if svcName == "" {
				continue
			}
			svcID := nodeID(NodeService, slice.Namespace, svcName)
			if _, ok := nodes[svcID]; !ok {
				nodes[svcID] = Node{
					ID: svcID, Kind: NodeService, Name: svcName, Namespace: slice.Namespace,
				}
			}
			for _, ep := range slice.Endpoints {
				podName, podNS := endpointPod(ep, slice.Namespace)
				if podName == "" {
					continue
				}
				podID := nodeID(NodePod, podNS, podName)
				if _, ok := nodes[podID]; !ok {
					nodes[podID] = Node{
						ID: podID, Kind: NodePod, Name: podName, Namespace: podNS,
					}
				}
				detail := fmt.Sprintf("EndpointSlice/%s", slice.Name)
				if len(slice.Ports) > 0 && slice.Ports[0].Port != nil {
					detail = fmt.Sprintf("%s port %d", detail, *slice.Ports[0].Port)
				}
				rep.Edges = append(rep.Edges, Edge{
					From: svcID, To: podID, Type: EdgeRoutes, Detail: detail, Source: SourceKubernetes,
				})
			}
		}
	}

	if req.IncludeNetworkPolicy {
		policies, err := client.NetworkingV1().NetworkPolicies(ns).List(ctx, metav1.ListOptions{Limit: limit})
		if err != nil {
			if apierrors.IsForbidden(err) {
				notes = append(notes, fmt.Sprintf("skipped NetworkPolicies: %v", err))
			} else {
				notes = append(notes, fmt.Sprintf("NetworkPolicies unavailable: %v", err))
			}
		} else {
			var svcList []corev1.Service
			if svcs != nil {
				svcList = svcs.Items
			}
			for _, np := range policies.Items {
				npID := nodeID(NodeNetworkPolicy, np.Namespace, np.Name)
				nodes[npID] = Node{
					ID: npID, Kind: NodeNetworkPolicy, Name: np.Name, Namespace: np.Namespace,
					Labels: copyLabels(np.Labels),
				}
				for _, svc := range svcList {
					if svc.Namespace != np.Namespace {
						continue
					}
					if networkPolicySelectsService(np, svc) {
						rep.Edges = append(rep.Edges, Edge{
							From: npID, To: nodeID(NodeService, svc.Namespace, svc.Name),
							Type: EdgeSelects, Detail: "podSelector", Source: SourceKubernetes,
						})
					}
				}
			}
		}
	} else {
		notes = append(notes, "NetworkPolicy edges omitted (pass includeNetworkPolicy to enable)")
	}

	if req.IncludeIngress {
		ings, err := client.NetworkingV1().Ingresses(ns).List(ctx, metav1.ListOptions{Limit: limit})
		if err != nil {
			if apierrors.IsForbidden(err) {
				notes = append(notes, fmt.Sprintf("skipped Ingresses: %v", err))
			} else {
				notes = append(notes, fmt.Sprintf("Ingresses unavailable: %v", err))
			}
		} else {
			for _, ing := range ings.Items {
				ingID := nodeID(NodeIngress, ing.Namespace, ing.Name)
				nodes[ingID] = Node{
					ID: ingID, Kind: NodeIngress, Name: ing.Name, Namespace: ing.Namespace,
					Labels: copyLabels(ing.Labels),
				}
				for _, rule := range ing.Spec.Rules {
					if rule.HTTP == nil {
						continue
					}
					for _, path := range rule.HTTP.Paths {
						svcName := path.Backend.Service
						if svcName == nil || strings.TrimSpace(svcName.Name) == "" {
							continue
						}
						svcID := nodeID(NodeService, ing.Namespace, svcName.Name)
						if _, ok := nodes[svcID]; !ok {
							nodes[svcID] = Node{
								ID: svcID, Kind: NodeService, Name: svcName.Name, Namespace: ing.Namespace,
							}
						}
						detail := "path /"
						if path.Path != "" {
							detail = "path " + path.Path
						}
						if rule.Host != "" {
							detail = rule.Host + " " + detail
						}
						rep.Edges = append(rep.Edges, Edge{
							From: ingID, To: svcID, Type: EdgeExposes, Detail: detail, Source: SourceKubernetes,
						})
					}
				}
				// Default backend
				if ing.Spec.DefaultBackend != nil && ing.Spec.DefaultBackend.Service != nil {
					svcName := ing.Spec.DefaultBackend.Service.Name
					if strings.TrimSpace(svcName) != "" {
						svcID := nodeID(NodeService, ing.Namespace, svcName)
						if _, ok := nodes[svcID]; !ok {
							nodes[svcID] = Node{
								ID: svcID, Kind: NodeService, Name: svcName, Namespace: ing.Namespace,
							}
						}
						rep.Edges = append(rep.Edges, Edge{
							From: ingID, To: svcID, Type: EdgeExposes, Detail: "defaultBackend", Source: SourceKubernetes,
						})
					}
				}
			}
		}
	} else {
		notes = append(notes, "Ingress edges omitted (pass includeIngress to enable)")
	}

	if req.IncludePVC {
		pvcs, err := client.CoreV1().PersistentVolumeClaims(ns).List(ctx, metav1.ListOptions{Limit: limit})
		if err != nil {
			if apierrors.IsForbidden(err) {
				notes = append(notes, fmt.Sprintf("skipped PVCs: %v", err))
			} else {
				notes = append(notes, fmt.Sprintf("PVCs unavailable: %v", err))
			}
		} else {
			for _, pvc := range pvcs.Items {
				pvcID := nodeID(NodePVC, pvc.Namespace, pvc.Name)
				nodes[pvcID] = Node{
					ID: pvcID, Kind: NodePVC, Name: pvc.Name, Namespace: pvc.Namespace,
					Labels: copyLabels(pvc.Labels),
				}
			}
		}
		pods, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{Limit: limit})
		if err != nil {
			if apierrors.IsForbidden(err) {
				notes = append(notes, fmt.Sprintf("skipped Pods for PVC mounts: %v", err))
			} else {
				notes = append(notes, fmt.Sprintf("Pods unavailable for PVC mounts: %v", err))
			}
		} else {
			for _, pod := range pods.Items {
				podID := nodeID(NodePod, pod.Namespace, pod.Name)
				for _, vol := range pod.Spec.Volumes {
					if vol.PersistentVolumeClaim == nil {
						continue
					}
					claim := strings.TrimSpace(vol.PersistentVolumeClaim.ClaimName)
					if claim == "" {
						continue
					}
					if _, ok := nodes[podID]; !ok {
						nodes[podID] = Node{
							ID: podID, Kind: NodePod, Name: pod.Name, Namespace: pod.Namespace,
							Labels: copyLabels(pod.Labels),
						}
					}
					pvcID := nodeID(NodePVC, pod.Namespace, claim)
					if _, ok := nodes[pvcID]; !ok {
						nodes[pvcID] = Node{
							ID: pvcID, Kind: NodePVC, Name: claim, Namespace: pod.Namespace,
						}
					}
					rep.Edges = append(rep.Edges, Edge{
						From: podID, To: pvcID, Type: EdgeMounts,
						Detail: "volume " + vol.Name, Source: SourceKubernetes,
					})
				}
			}
		}
	} else {
		notes = append(notes, "PVC mount edges omitted (pass includePVC to enable)")
	}

	// Deduplicate edges.
	edgeSeen := map[string]struct{}{}
	unique := make([]Edge, 0, len(rep.Edges))
	for _, e := range rep.Edges {
		k := e.From + "|" + e.To + "|" + e.Type + "|" + e.Source
		if _, ok := edgeSeen[k]; ok {
			continue
		}
		edgeSeen[k] = struct{}{}
		unique = append(unique, e)
	}
	rep.Edges = unique

	ids := make([]string, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		rep.Nodes = append(rep.Nodes, nodes[id])
		if int64(len(rep.Nodes)) >= limit {
			notes = append(notes, fmt.Sprintf("truncated at %d nodes", limit))
			break
		}
	}
	sort.Slice(rep.Edges, func(i, j int) bool {
		if rep.Edges[i].From != rep.Edges[j].From {
			return rep.Edges[i].From < rep.Edges[j].From
		}
		return rep.Edges[i].To < rep.Edges[j].To
	})
	if int64(len(rep.Edges)) > limit {
		rep.Edges = rep.Edges[:limit]
		notes = append(notes, fmt.Sprintf("truncated at %d edges", limit))
	}

	rep.Notes = notes
	refreshSummary(&rep)
	return rep, nil
}

func nodeID(kind, namespace, name string) string {
	if namespace == "" {
		return kind + "/" + name
	}
	return namespace + "/" + kind + "/" + name
}

func copyLabels(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func endpointPod(ep discoveryv1.Endpoint, sliceNS string) (name, namespace string) {
	if ep.TargetRef == nil {
		return "", ""
	}
	if !strings.EqualFold(ep.TargetRef.Kind, "Pod") {
		return "", ""
	}
	name = ep.TargetRef.Name
	namespace = ep.TargetRef.Namespace
	if namespace == "" {
		namespace = sliceNS
	}
	return name, namespace
}

func networkPolicySelectsService(np networkingv1.NetworkPolicy, svc corev1.Service) bool {
	sel, err := metav1.LabelSelectorAsSelector(&np.Spec.PodSelector)
	if err != nil {
		return false
	}
	// Empty selector matches all pods in the namespace → all services.
	if sel.Empty() {
		return true
	}
	if len(svc.Spec.Selector) == 0 {
		return false
	}
	return sel.Matches(labels.Set(svc.Spec.Selector))
}
