package graph

import (
	"fmt"
	"sort"
	"strings"
)

// ImpactNotes returns a short blast/impact hint from depends_on / allows edges
// touching the named target (RT-015). Hostnames / Service names only.
func ImpactNotes(rep Report, namespace, kind, name string) string {
	namespace = strings.TrimSpace(namespace)
	kind = strings.TrimSpace(kind)
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	targetIDs := map[string]struct{}{}
	primary := nodeID(kind, namespace, name)
	targetIDs[primary] = struct{}{}
	// Also match Service/Pod nodes with same name in ns.
	for _, n := range rep.Nodes {
		if n.Namespace != namespace {
			continue
		}
		nLower := strings.ToLower(n.Name)
		tLower := strings.ToLower(name)
		if nLower == tLower || strings.HasPrefix(nLower, tLower+"-") {
			if n.Kind == NodeService || n.Kind == NodePod || n.Kind == kind || strings.EqualFold(n.Kind, kind) {
				targetIDs[n.ID] = struct{}{}
			}
		}
	}
	// Pods that route from a Service named like the target.
	for _, e := range rep.Edges {
		if e.Type != EdgeRoutes {
			continue
		}
		if _, ok := targetIDs[e.From]; ok {
			targetIDs[e.To] = struct{}{}
		}
	}

	deps := map[string]struct{}{}
	for _, e := range rep.Edges {
		if e.Type != EdgeDependsOn && e.Type != EdgeAllows {
			continue
		}
		_, fromHit := targetIDs[e.From]
		_, toHit := targetIDs[e.To]
		if !fromHit && !toHit {
			continue
		}
		other := e.To
		if toHit && !fromHit {
			other = e.From
		}
		label := other
		for _, n := range rep.Nodes {
			if n.ID == other {
				if n.Kind == NodeExternalHost {
					label = n.Name
				} else {
					label = n.Kind + "/" + n.Name
				}
				break
			}
		}
		deps[label] = struct{}{}
	}
	if len(deps) == 0 {
		return ""
	}
	keys := make([]string, 0, len(deps))
	for k := range deps {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 5 {
		keys = keys[:5]
	}
	return fmt.Sprintf("graph depends_on/allows: %s", strings.Join(keys, ", "))
}
