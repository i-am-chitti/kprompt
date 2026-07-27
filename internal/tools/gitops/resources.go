package gitops

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

// ResourceDrift is one Argo CD status.resources entry that is not Synced.
type ResourceDrift struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace,omitempty"`
	Status     string `json:"status,omitempty"`
	Health     string `json:"health,omitempty"`
}

// ListResourceDrifts returns OutOfSync (or otherwise non-Synced) child resources
// from an Argo CD Application. Flux apps return nil (no per-resource inventory in MVP).
func ListResourceDrifts(ctx context.Context, cfg *rest.Config, app AppStatus) ([]ResourceDrift, error) {
	if !strings.EqualFold(app.Engine, "argocd") && !strings.EqualFold(app.Engine, "argo") {
		return nil, nil
	}
	if cfg == nil {
		return nil, fmt.Errorf("gitops resource drifts: rest config is nil")
	}
	dc, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	obj, err := dc.Resource(ApplicationGVR).Namespace(app.Namespace).Get(ctx, app.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return resourceDriftsFromApp(obj), nil
}

func resourceDriftsFromApp(obj *unstructured.Unstructured) []ResourceDrift {
	if obj == nil {
		return nil
	}
	raw, ok, _ := unstructured.NestedSlice(obj.Object, "status", "resources")
	if !ok {
		return nil
	}
	out := make([]ResourceDrift, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		status, _ := m["status"].(string)
		status = strings.TrimSpace(status)
		if status == "" || strings.EqualFold(status, "Synced") {
			continue
		}
		kind, _ := m["kind"].(string)
		name, _ := m["name"].(string)
		if strings.TrimSpace(kind) == "" || strings.TrimSpace(name) == "" {
			continue
		}
		ns, _ := m["namespace"].(string)
		api, _ := m["version"].(string)
		if g, _ := m["group"].(string); g != "" {
			if api != "" {
				api = g + "/" + api
			} else {
				api = g
			}
		}
		health := ""
		if hm, ok, _ := unstructured.NestedString(m, "health", "status"); ok {
			health = hm
		}
		out = append(out, ResourceDrift{
			APIVersion: api,
			Kind:       kind,
			Name:       name,
			Namespace:  ns,
			Status:     status,
			Health:     health,
		})
	}
	return out
}
