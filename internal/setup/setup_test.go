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
	plan, err := BuildPlan(reg, Options{Profile: ProfilePlatform, DryRun: true})
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

func TestBuildPlanOnlyFilter(t *testing.T) {
	reg := tools.NewRegistry([]tools.Result{
		{ID: tools.IDKubernetes, Status: tools.StatusAvailable, Detail: "ok"},
		{ID: tools.IDHelm, Status: tools.StatusUnavailable, Detail: "missing"},
		{ID: tools.IDArgoWorkflows, Status: tools.StatusUnavailable, Detail: "missing"},
		{ID: tools.IDPrometheus, Status: tools.StatusUnavailable, Detail: "missing"},
	})
	plan, err := BuildPlan(reg, Options{
		Profile: ProfilePlatform,
		Only:    []string{"prom", "helm"},
		DryRun:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 2 || plan.Needed != 2 {
		t.Fatalf("%+v", plan)
	}
	if plan.Steps[0].ID != "prometheus" || plan.Steps[1].ID != "helm" {
		t.Fatalf("order/ids=%+v", plan.Steps)
	}
	if len(plan.Only) != 2 {
		t.Fatalf("only=%v", plan.Only)
	}
}

func TestBuildPlanOnlyOutsideProfile(t *testing.T) {
	reg := tools.NewRegistry([]tools.Result{
		{ID: tools.IDHelm, Status: tools.StatusAvailable, Detail: "ok"},
	})
	_, err := BuildPlan(reg, Options{Profile: ProfileMinimal, Only: []string{"grafana"}})
	if err == nil || !strings.Contains(err.Error(), "not in the selected profile") {
		t.Fatalf("err=%v", err)
	}
}

func TestNormalizeOnly(t *testing.T) {
	got, err := NormalizeOnly([]string{"argo", "prom,helm", "helm"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != "argo-workflows" || got[1] != "prometheus" || got[2] != "helm" {
		t.Fatalf("%v", got)
	}
	_, err = NormalizeOnly([]string{"nope"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestProfilesCatalog(t *testing.T) {
	ps := Profiles()
	if len(ps) != 3 {
		t.Fatalf("%d", len(ps))
	}
	doc := ProfilesDoc()
	if !strings.Contains(doc, "minimal") || !strings.Contains(doc, "helm, argo-workflows, prometheus") {
		t.Fatalf("%s", doc)
	}
}

