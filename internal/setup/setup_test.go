package setup

import (
	"strings"
	"testing"

	"github.com/kprompt/kprompt/internal/tools"
)

func TestBuildPlanPlatformNeeded(t *testing.T) {
	reg := tools.NewRegistry([]tools.Result{
		{ID: tools.IDKubernetes, Status: tools.StatusAvailable, Detail: "context: kind"},
		{ID: tools.IDHelm, Status: tools.StatusUnavailable, Detail: "helm not on PATH", Hint: tools.MissingHint(tools.IDHelm)},
		{ID: tools.IDArgoWorkflows, Status: tools.StatusUnavailable, Detail: "Workflow CRD missing"},
		{ID: tools.IDPrometheus, Status: tools.StatusUnavailable, Detail: "URL not set"},
	})
	plan, err := BuildPlan(reg, Options{Profile: ProfilePlatform})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.DryRun {
		t.Fatal("must be dry-run")
	}
	if plan.Needed != 3 {
		t.Fatalf("needed=%d steps=%+v", plan.Needed, plan.Steps)
	}
	byID := map[string]Step{}
	for _, s := range plan.Steps {
		byID[s.ID] = s
	}
	if byID["helm"].Lane != LaneHost || byID["helm"].Status != StatusNeeded {
		t.Fatalf("helm=%+v", byID["helm"])
	}
	if byID["argo-workflows"].Lane != LaneCluster || byID["argo-workflows"].Status != StatusNeeded {
		t.Fatalf("argo=%+v", byID["argo-workflows"])
	}
	if byID["prometheus"].Lane != LaneCluster || byID["prometheus"].Status != StatusNeeded {
		t.Fatalf("prom=%+v", byID["prometheus"])
	}
	if len(byID["helm"].Commands) == 0 || !strings.Contains(byID["helm"].Commands[1], "brew") {
		t.Fatalf("helm commands=%v", byID["helm"].Commands)
	}
}

func TestBuildPlanMinimalReady(t *testing.T) {
	reg := tools.NewRegistry([]tools.Result{
		{ID: tools.IDHelm, Status: tools.StatusAvailable, Detail: "/usr/local/bin/helm"},
	})
	plan, err := BuildPlan(reg, Options{Profile: ProfileMinimal})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Needed != 0 || plan.Ready != 1 {
		t.Fatalf("%+v", plan)
	}
	if !strings.Contains(plan.Summary, "nothing to install") {
		t.Fatalf("summary=%s", plan.Summary)
	}
}

func TestBuildPlanBlocksClusterWithoutKube(t *testing.T) {
	reg := tools.NewRegistry([]tools.Result{
		{ID: tools.IDKubernetes, Status: tools.StatusUnavailable, Detail: "no kube"},
		{ID: tools.IDHelm, Status: tools.StatusAvailable, Detail: "helm"},
		{ID: tools.IDArgoWorkflows, Status: tools.StatusUnavailable, Detail: "missing"},
		{ID: tools.IDPrometheus, Status: tools.StatusAvailable, Detail: "http://prom"},
	})
	plan, err := BuildPlan(reg, Options{Profile: ProfilePlatform})
	if err != nil {
		t.Fatal(err)
	}
	var argo Step
	for _, s := range plan.Steps {
		if s.ID == "argo-workflows" {
			argo = s
		}
	}
	if argo.Status != StatusBlocked {
		t.Fatalf("argo=%+v", argo)
	}
}

func TestNormalizeProfile(t *testing.T) {
	p, err := NormalizeProfile("")
	if err != nil || p != ProfilePlatform {
		t.Fatalf("%q %v", p, err)
	}
	_, err = NormalizeProfile("nope")
	if err == nil {
		t.Fatal("expected error")
	}
}
