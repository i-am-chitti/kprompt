package runworker

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/kprompt/kprompt/internal/config"
	"github.com/kprompt/kprompt/internal/output"
	"github.com/kprompt/kprompt/internal/pipeline"
	"github.com/kprompt/kprompt/internal/team"
)

// Execute runs the local plan pipeline for a claimed Team job (never auto-applies).
func Execute(ctx context.Context, job team.RunJob) (team.PostRunResultInput, error) {
	file, err := config.LoadFile()
	if err != nil {
		return team.PostRunResultInput{}, err
	}
	cfg := config.Merge(file, "", "", job.ContextHint, job.Namespace, false, job.Prompt)
	cfg.Output = "json"
	if job.Namespace != "" {
		cfg.NamespaceFromCLI = true
	}
	if job.ContextHint != "" {
		cfg.ContextFromCLI = true
	}
	team.ApplyOrgContextPolicy(&cfg)

	var last *output.PlanResult
	var buf bytes.Buffer
	tty := false
	err = pipeline.RunWith(ctx, cfg, io.MultiWriter(&buf, os.Stderr), pipeline.Deps{
		IsTerminal: &tty,
		Confirm:    func(io.Writer) (bool, error) { return false, nil },
		OnResult: func(doc output.PlanResult) {
			cp := doc
			last = &cp
		},
	})
	if last == nil {
		msg := "no plan result"
		if err != nil {
			msg = err.Error()
		}
		return team.PostRunResultInput{Status: "failed", Error: msg, Summary: "bridge failed"}, err
	}

	body, mErr := json.Marshal(last)
	if mErr != nil {
		return team.PostRunResultInput{Status: "failed", Error: mErr.Error()}, mErr
	}
	summary := last.Plan.Summary
	if summary == "" {
		summary = "plan"
	}
	mode := strings.ToLower(strings.TrimSpace(job.ApproveMode))
	status := "succeeded"
	switch {
	case last.Risk.Denied:
		status = "denied"
	case mode == "require_approve" && last.Plan.RequiresApproval:
		status = "awaiting_approve"
	case mode == "auto_if_policy_allows" && last.Plan.RequiresApproval:
		status = "awaiting_approve"
	}
	return team.PostRunResultInput{
		Status:  status,
		Summary: summary,
		Risk:    last.Risk.Level,
		Body:    body,
	}, nil
}
