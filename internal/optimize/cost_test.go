package optimize

import (
	"strings"
	"testing"
	"time"
)

func TestApplyCostNotesIdle(t *testing.T) {
	pct := 3.0
	rep := Report{
		Window: "1h",
		Workloads: []Workload{{
			Kind: WorkloadDeployment, Namespace: "prod", Name: "api",
			Replicas: 2, CPURequest: "1", MemoryRequest: "1Gi",
		}},
		Idle: []IdleWorkload{{
			Kind: WorkloadDeployment, Namespace: "prod", Name: "api",
			CPUOfRequestPct: &pct, Idle: true,
			Message: "Deployment/api averaged 3% CPU of request",
		}},
		Findings: []Finding{{
			Code: "optimize.idle.workload", Severity: "medium",
			Title: "Underutilized workload",
			Message: "Deployment/api averaged 3% CPU of request",
			Resource: "Deployment/api", Namespace: "prod",
		}},
	}
	ApplyCostNotes(&rep, time.Hour)
	if rep.Idle[0].CostNote == "" || !strings.Contains(rep.Idle[0].CostNote, "estimate") {
		t.Fatalf("costNote=%q", rep.Idle[0].CostNote)
	}
	if !strings.Contains(rep.Idle[0].CostNote, "not a bill") {
		t.Fatalf("must label estimate: %s", rep.Idle[0].CostNote)
	}
	if !strings.Contains(rep.Idle[0].Message, "estimate") {
		t.Fatalf("message=%s", rep.Idle[0].Message)
	}
	found := false
	for _, f := range rep.Findings {
		if f.Code == "optimize.cost.notes" {
			found = true
			if !strings.Contains(f.Message, "Not a cloud bill") {
				t.Fatalf("%s", f.Message)
			}
		}
	}
	if !found {
		t.Fatal("missing rollup finding")
	}
}

func TestApplyCostNotesRightsizingLower(t *testing.T) {
	rep := Report{
		Window: "1h",
		Workloads: []Workload{{
			Kind: WorkloadDeployment, Namespace: "prod", Name: "api",
			Replicas: 1, CPURequest: "1000m",
		}},
		Rightsizing: []RightsizingDelta{{
			Kind: WorkloadDeployment, Namespace: "prod", Name: "api",
			Resource: "cpu", Field: "request",
			Current: "1", Suggested: "200m", Direction: "lower",
			Message: "lower CPU request 1→200m",
		}},
	}
	ApplyCostNotes(&rep, time.Hour)
	if rep.Rightsizing[0].CostNote == "" || !strings.Contains(rep.Rightsizing[0].CostNote, "$") {
		t.Fatalf("%q", rep.Rightsizing[0].CostNote)
	}
}

func TestApplyCostNotesNoFakeWithoutSignals(t *testing.T) {
	rep := Report{Window: "1h", Workloads: []Workload{{Name: "api"}}}
	ApplyCostNotes(&rep, time.Hour)
	for _, f := range rep.Findings {
		if f.Code == "optimize.cost.notes" {
			t.Fatalf("unexpected cost finding: %+v", f)
		}
	}
}

func TestApplyCostNotesSkipsRaiseAndLimits(t *testing.T) {
	rep := Report{
		Workloads: []Workload{{
			Kind: WorkloadDeployment, Namespace: "prod", Name: "api",
			Replicas: 1, CPURequest: "200m",
		}},
		Rightsizing: []RightsizingDelta{
			{
				Kind: WorkloadDeployment, Namespace: "prod", Name: "api",
				Resource: "cpu", Field: "request",
				Current: "200m", Suggested: "1", Direction: "raise",
				Message: "raise",
			},
			{
				Kind: WorkloadDeployment, Namespace: "prod", Name: "api",
				Resource: "cpu", Field: "limit",
				Current: "2", Suggested: "1", Direction: "lower",
				Message: "lower limit",
			},
		},
	}
	ApplyCostNotes(&rep, time.Hour)
	if rep.Rightsizing[0].CostNote != "" || rep.Rightsizing[1].CostNote != "" {
		t.Fatalf("%+v", rep.Rightsizing)
	}
}

func TestApplyCostNotesMissingRequestsSkip(t *testing.T) {
	pct := 2.0
	rep := Report{
		Workloads: []Workload{{
			Kind: WorkloadDeployment, Namespace: "prod", Name: "api",
			Replicas: 1, // no CPU/memory request strings
		}},
		Idle: []IdleWorkload{{
			Kind: WorkloadDeployment, Namespace: "prod", Name: "api",
			CPUOfRequestPct: &pct, Idle: true, Message: "idle",
		}},
	}
	ApplyCostNotes(&rep, time.Hour)
	if rep.Idle[0].CostNote != "" {
		t.Fatalf("expected no fake cost, got %q", rep.Idle[0].CostNote)
	}
}
