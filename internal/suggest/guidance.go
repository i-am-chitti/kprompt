package suggest

import (
	"fmt"

	"github.com/kprompt/kprompt/internal/cluster"
)

// suggestGuidance returns copy-pasteable follow-ups for scheduling/storage findings.
// Never invents StorageClasses, node pools, tolerations, or capacity — ops fixes only.
func suggestGuidance(rep cluster.ExplainReport, f cluster.Finding) *Suggestion {
	target := rep.Target
	if target == "" {
		target = "workload"
	}
	switch f.Code {
	case "Pending", "Unschedulable", "UnknownSchedule":
		return &Suggestion{
			Code:    f.Code,
			Title:   "Inspect scheduling",
			Prompt:  fmt.Sprintf(`describe %s`, target),
			Summary: "Read pod events / conditions — no invented affinity or node patches",
		}
	case "PVCMissing":
		return &Suggestion{
			Code:    f.Code,
			Title:   "Create or fix the PVC",
			Prompt:  fmt.Sprintf(`describe %s`, target),
			Summary: "Volume references a missing claim — create the PVC or fix the volume name (no auto-delete)",
		}
	case "PVCPending", "MissingStorageClass", "StorageClassError":
		return &Suggestion{
			Code:    f.Code,
			Title:   "Fix storage binding",
			Prompt:  fmt.Sprintf(`describe %s`, target),
			Summary: "Point the PVC at an existing StorageClass or create the class — tags/classes are never invented",
		}
	case "NodeSelector":
		return &Suggestion{
			Code:    f.Code,
			Title:   "Relax or satisfy node selector",
			Prompt:  fmt.Sprintf(`describe %s`, target),
			Summary: "No nodes match selector/affinity — adjust labels on nodes or the pod template manually",
		}
	case "NoGPUNodes":
		return &Suggestion{
			Code:    f.Code,
			Title:   "Add GPU capacity or drop GPU request",
			Prompt:  fmt.Sprintf(`describe %s`, target),
			Summary: "Cluster has no GPU nodes — add capacity or remove the GPU request/affinity",
		}
	case "TaintToleration":
		return &Suggestion{
			Code:    f.Code,
			Title:   "Add a matching toleration",
			Prompt:  fmt.Sprintf(`describe %s`, target),
			Summary: "Node taints block the pod — add an explicit toleration or untaint the node (ops change)",
		}
	case "ResourcePressure":
		return &Suggestion{
			Code:    f.Code,
			Title:   "Free capacity or lower requests",
			Prompt:  fmt.Sprintf(`describe %s`, target),
			Summary: "Insufficient CPU/memory/pod slots — scale down neighbors or lower requests after review",
		}
	default:
		return &Suggestion{
			Code:    f.Code,
			Title:   "Inspect workload",
			Prompt:  fmt.Sprintf(`describe %s`, target),
			Summary: f.Message,
		}
	}
}
