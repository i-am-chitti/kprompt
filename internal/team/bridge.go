package team

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// BridgeOptions configures the enrolled CLI bridge poll loop (A-052).
type BridgeOptions struct {
	WorkerLabel string
	Interval    time.Duration
	Stdout      func(string)
	Sleep       func(time.Duration)
	// Execute runs one claimed job. Required (set by CLI to avoid import cycles).
	Execute func(ctx context.Context, job RunJob) (PostRunResultInput, error)
	// Claim / Post injectables for tests.
	Claim      func(ctx context.Context, label string) (RunJob, bool, error)
	Post       func(ctx context.Context, runID string, in PostRunResultInput) (RunJob, error)
	PullPolicy func(ctx context.Context) error
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
	claim := opt.Claim
	if claim == nil {
		claim = client.ClaimRun
	}
	post := opt.Post
	if post == nil {
		post = client.PostRunResult
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
		} else {
			log(fmt.Sprintf("Posted %s → %s", job.ID, result.Status))
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
