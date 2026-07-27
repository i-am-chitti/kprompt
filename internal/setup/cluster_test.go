package setup

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/kprompt/kprompt/internal/planner"
	"github.com/kprompt/kprompt/internal/safety"
	"github.com/kprompt/kprompt/internal/tools"
)

func TestBuildClusterPlanArgo(t *testing.T) {
	plan, err := BuildClusterPlan(Step{Component: "argo-workflows"}, ClusterApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.RequiresApproval || len(plan.Actions) < 2 {
		t.Fatalf("%+v", plan)
	}
	risk := safety.EvaluatePlan(plan)
	if risk.Denied {
		t.Fatalf("denied: %s", risk.Message)
	}
	if err := assertInstallOnly(plan); err != nil {
		t.Fatal(err)
	}
}

func TestBuildClusterPlanPrometheus(t *testing.T) {
	plan, err := BuildClusterPlan(Step{Component: "prometheus"}, ClusterApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, a := range plan.Actions {
		joined += strings.Join(a.Command, " ") + " "
	}
	if !strings.Contains(joined, "helm install") || strings.Contains(joined, "uninstall") {
		t.Fatalf("%s", joined)
	}
	if risk := safety.EvaluatePlan(plan); risk.Denied {
		t.Fatal(risk.Message)
	}
}

func TestAssertSafeArgvDeniesWipe(t *testing.T) {
	err := assertSafeArgv([]string{"helm", "uninstall", "x", "--all"})
	if err == nil {
		t.Fatal("expected deny")
	}
	err = assertSafeArgv([]string{"kubectl", "delete", "namespace", "argo"})
	if err == nil {
		t.Fatal("expected deny")
	}
}

func TestApplyClusterArgo(t *testing.T) {
	plan := Plan{Steps: []Step{{
		ID: "argo-workflows", Component: "argo-workflows", Lane: LaneCluster, Status: StatusNeeded,
	}}}
	r := &fakeRunner{paths: map[string]string{"kubectl": "/usr/bin/kubectl"}}
	var buf bytes.Buffer
	rep, err := ApplyCluster(context.Background(), plan, ClusterApplyOptions{Runner: r}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Applied) != 1 || rep.Applied[0].Status != "installed" {
		t.Fatalf("%+v ran=%v", rep, r.ran)
	}
	if len(r.ran) < 2 {
		t.Fatalf("ran=%v", r.ran)
	}
}

func TestApplyClusterDeniesUninstallPlan(t *testing.T) {
	bad := planner.ExecutionPlan{
		Actions: []planner.Action{{
			Op: planner.OpHelmInstall, Backend: "helm",
			Command: []string{"helm", "uninstall", "x", "--all"},
		}},
	}
	if err := assertInstallOnly(bad); err == nil {
		t.Fatal("expected deny")
	}
}

func TestClusterNeeded(t *testing.T) {
	plan := Plan{Steps: []Step{
		{Lane: LaneCluster, Status: StatusNeeded, Component: "argo-workflows"},
		{Lane: LaneConfig, Status: StatusNeeded, Component: "grafana"},
		{Lane: LaneCluster, Status: StatusReady, Component: "prometheus"},
	}}
	got := ClusterNeeded(plan)
	if len(got) != 1 || got[0].Component != "argo-workflows" {
		t.Fatalf("%+v", got)
	}
}

func TestPlatformPrometheusIsClusterLane(t *testing.T) {
	reg := tools.NewRegistry([]tools.Result{
		{ID: tools.IDKubernetes, Status: tools.StatusAvailable},
		{ID: tools.IDHelm, Status: tools.StatusAvailable},
		{ID: tools.IDArgoWorkflows, Status: tools.StatusUnavailable},
		{ID: tools.IDPrometheus, Status: tools.StatusUnavailable, Detail: "URL not set"},
	})
	plan, err := BuildPlan(reg, Options{Profile: ProfilePlatform})
	if err != nil {
		t.Fatal(err)
	}
	var prom Step
	for _, s := range plan.Steps {
		if s.Component == "prometheus" {
			prom = s
		}
	}
	if prom.Lane != LaneCluster || prom.Status != StatusNeeded {
		t.Fatalf("%+v", prom)
	}
}

