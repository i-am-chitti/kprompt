package team

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// BridgeOptions configures the enrolled CLI bridge poll loop (A-052 / A-054).
type BridgeOptions struct {
	WorkerLabel string
	Interval    time.Duration
	Stdout      func(string)
	Sleep       func(time.Duration)
	// Execute plans without apply. Required.
	Execute func(ctx context.Context, job RunJob) (PostRunResultInput, error)
	// ExecuteApply applies after in-app approve (optional; skips apply wait if nil).
	ExecuteApply func(ctx context.Context, job RunJob) (PostRunResultInput, error)
	Claim        func(ctx context.Context, label string) (RunJob, bool, error)
	Post         func(ctx context.Context, runID string, in PostRunResultInput) (RunJob, error)
	Get          func(ctx context.Context, runID string) (RunJob, error)
	PullPolicy   func(ctx context.Context) error
	// ApproveWait is how long to wait for in-app approve (default 30m).
	ApproveWait time.Duration
}

// Listen polls the control plane for run jobs until ctx is cancelled.
func Listen(ctx context.Context, client *Client, opt BridgeOptions) error {
	if opt.Execute == nil {
		return fmt.Errorf("bridge execute function required")
	}
	log := opt.Stdout
	if log == nil {
		log = func(string) {}
	}
	sleep := opt.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	interval := opt.Interval
	if interval <= 0 {
		interval = 3 * time.Second
	}
	approveWait := opt.ApproveWait
	if approveWait <= 0 {
		approveWait = 30 * time.Minute
	}
	claim := opt.Claim
	if claim == nil {
		claim = client.ClaimRun
	}
	post := opt.Post
	if post == nil {
		post = client.PostRunResult
	}
	get := opt.Get
	if get == nil {
		get = client.GetRun
	}
	pull := opt.PullPolicy
	if pull == nil {
		pull = func(ctx context.Context) error {
			_, err := PullPolicy(ctx)
			return err
		}
	}

	label := strings.TrimSpace(opt.WorkerLabel)
	if label == "" {
		host, _ := os.Hostname()
		label = "bridge-" + host
	}
	log(fmt.Sprintf("Listening for Team run jobs as %q (poll %s)…", label, interval))

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		job, ok, err := claim(ctx, label)
		if err != nil {
			log(fmt.Sprintf("claim error: %v", err))
			sleep(interval)
			continue
		}
		if !ok {
			sleep(interval)
			continue
		}
		log(fmt.Sprintf("Claimed %s — %q", job.ID, truncate(job.Prompt, 72)))
		if err := pull(ctx); err != nil {
			log(fmt.Sprintf("policy pull: %v (continuing with cache)", err))
		}
		_, _ = post(ctx, job.ID, PostRunResultInput{Status: "running", Summary: "bridge executing"})
		result, err := opt.Execute(ctx, job)
		if err != nil && result.Status == "" {
			result = PostRunResultInput{
				Status:  "failed",
				Error:   err.Error(),
				Summary: "bridge execution failed",
			}
		}
		if _, postErr := post(ctx, job.ID, result); postErr != nil {
			log(fmt.Sprintf("result post error: %v", postErr))
			continue
		}
		log(fmt.Sprintf("Posted %s → %s", job.ID, result.Status))

		if result.Status != "awaiting_approve" || opt.ExecuteApply == nil {
			continue
		}
		log(fmt.Sprintf("Waiting for in-app approve on %s…", job.ID))
		decision, waitErr := waitForDecision(ctx, get, sleep, interval, job.ID, approveWait)
		if waitErr != nil {
			log(fmt.Sprintf("approve wait: %v", waitErr))
			continue
		}
		switch decision.Status {
		case "denied", "cancelled", "failed", "succeeded":
			log(fmt.Sprintf("%s settled as %s", job.ID, decision.Status))
			continue
		case "approved":
			log(fmt.Sprintf("Approved %s — applying locally…", job.ID))
			_, _ = post(ctx, job.ID, PostRunResultInput{Status: "running", Summary: "bridge applying"})
			applied, applyErr := opt.ExecuteApply(ctx, job)
			if applyErr != nil && applied.Status == "" {
				applied = PostRunResultInput{
					Status:  "failed",
					Error:   applyErr.Error(),
					Summary: "bridge apply failed",
				}
			}
			if _, postErr := post(ctx, job.ID, applied); postErr != nil {
				log(fmt.Sprintf("apply result post error: %v", postErr))
			} else {
				log(fmt.Sprintf("Posted %s → %s", job.ID, applied.Status))
			}
		default:
			log(fmt.Sprintf("%s unexpected status %s after wait", job.ID, decision.Status))
		}
	}
}

func waitForDecision(
	ctx context.Context,
	get func(context.Context, string) (RunJob, error),
	sleep func(time.Duration),
	interval time.Duration,
	runID string,
	deadline time.Duration,
) (RunJob, error) {
	until := time.Now().Add(deadline)
	for {
		if err := ctx.Err(); err != nil {
			return RunJob{}, err
		}
		if time.Now().After(until) {
			return RunJob{}, fmt.Errorf("timed out waiting for approve on %s", runID)
		}
		job, err := get(ctx, runID)
		if err != nil {
			sleep(interval)
			continue
		}
		switch job.Status {
		case "awaiting_approve", "awaiting_second_approve":
			sleep(interval)
			continue
		default:
			return job, nil
		}
	}
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
