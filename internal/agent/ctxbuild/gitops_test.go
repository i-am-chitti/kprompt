package ctxbuild

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kprompt/kprompt/internal/incident"
	"github.com/kprompt/kprompt/internal/tools/gitops"
)

type stubGitOps struct {
	rep gitops.StatusReport
	err error
}

func (s stubGitOps) NamespaceStatus(context.Context, string) (gitops.StatusReport, error) {
	return s.rep, s.err
}

func TestEnrichGitOpsEvidence(t *testing.T) {
	b := &Builder{
		GitOps: stubGitOps{rep: gitops.StatusReport{
			Summary: "1 app(s)",
			Apps: []gitops.AppStatus{{
				Engine:   "argocd",
				Kind:     "Application",
				Name:     "api",
				Namespace: "payments",
				Sync:     "OutOfSync",
				Health:   "Degraded",
				Revision: "abc123",
				History:  []string{"abc123", "def456"},
			}},
		}},
	}
	out := &AgentContext{Namespace: "payments"}
	b.enrichGitOps(context.Background(), out, "api")
	if len(out.GitOps) < 2 {
		t.Fatalf("expected sync + history evidence, got %+v", out.GitOps)
	}
	if out.GitOps[0].Type != incident.EvidenceGitOps || out.GitOps[0].Reason != "out_of_sync" {
		t.Fatalf("%+v", out.GitOps[0])
	}
	foundHist := false
	for _, e := range out.GitOps {
		if e.Reason == "deploy_history" {
			foundHist = true
			if !strings.Contains(e.Message, "abc123") {
				t.Fatalf("history msg=%q", e.Message)
			}
		}
	}
	if !foundHist {
		t.Fatal("missing deploy_history")
	}
	_ = time.Now()
}

func TestEnrichGitOpsNilQuerierNoDegrade(t *testing.T) {
	b := &Builder{}
	out := &AgentContext{Namespace: "payments"}
	b.enrichGitOps(context.Background(), out, "api")
	if len(out.Degraded) != 0 || len(out.GitOps) != 0 {
		t.Fatalf("opt-in skip should be silent: %+v", out)
	}
}

func TestEnrichGitOpsUnavailableDegrades(t *testing.T) {
	b := &Builder{GitOps: stubGitOps{rep: gitops.StatusReport{
		Summary: "GitOps controllers not available",
		Notes:   []string{"no Flux or Argo CD CRDs detected"},
	}}}
	out := &AgentContext{Namespace: "payments"}
	b.enrichGitOps(context.Background(), out, "api")
	if len(out.Degraded) == 0 || out.Degraded[0] != "gitops" {
		t.Fatalf("degraded=%v", out.Degraded)
	}
}
