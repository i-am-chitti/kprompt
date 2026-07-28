package planner

import (
	"fmt"
	"strings"

	"github.com/kprompt/kprompt/internal/intent"
	"github.com/kprompt/kprompt/internal/tools/hpa"
)

func buildHPA(in intent.Intent, ns string) (ExecutionPlan, error) {
	target := strings.TrimSpace(in.Target.Name)
	if t, ok := in.StringParam("target"); ok {
		target = t
	}
	if target == "" {
		return ExecutionPlan{}, fmt.Errorf("hpa intent requires a workload name (Deployment to scale)")
	}

	name := hpa.DefaultHPAName(target)
	if n, ok := in.StringParam("hpaName"); ok {
		name = n
	}

	minRep := int32(1)
	if v, ok := int32Param(in, "minReplicas"); ok {
		minRep = v
	}
	maxRep := int32(10)
	if v, ok := int32Param(in, "maxReplicas"); ok {
		maxRep = v
	}
	cpu := int32(70)
	if v, ok := int32Param(in, "cpuPercent"); ok {
		cpu = v
	} else if v, ok := int32Param(in, "cpu"); ok {
		cpu = v
	}
	var mem int32
	if v, ok := int32Param(in, "memoryPercent"); ok {
		mem = v
	} else if v, ok := int32Param(in, "memory"); ok {
		mem = v
	}

	manifest, summary, err := hpa.Generate(hpa.Request{
		Name:          name,
		Namespace:     ns,
		TargetName:    target,
		TargetKind:    "Deployment",
		MinReplicas:   minRep,
		MaxReplicas:   maxRep,
		CPUPercent:    cpu,
		MemoryPercent: mem,
	})
	if err != nil {
		return ExecutionPlan{}, err
	}

	return ExecutionPlan{
		Intent: in,
		Actions: []Action{{
			Op:       OpCreate,
			Backend:  "kubernetes",
			Manifest: manifest,
			Diff:     summary,
			Object: ObjectRef{
				APIVersion: "autoscaling/v2",
				Kind:       "HorizontalPodAutoscaler",
				Name:       name,
				Namespace:  ns,
			},
		}},
		Summary:          summary,
		RequiresApproval: true,
	}, nil
}

func int32Param(in intent.Intent, key string) (int32, bool) {
	v, ok := in.Params[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int32(n), true
	case int:
		return int32(n), true
	case int32:
		return n, true
	case int64:
		return int32(n), true
	default:
		return 0, false
	}
}
