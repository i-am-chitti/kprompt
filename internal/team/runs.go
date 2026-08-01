package team

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// RunJob is a control-plane prompt job for the CLI bridge (A-051 / A-052).
type RunJob struct {
	ID             string     `json:"id"`
	OrgID          string     `json:"org_id"`
	Prompt         string     `json:"prompt"`
	Namespace      string     `json:"namespace,omitempty"`
	ContextHint    string     `json:"context_hint,omitempty"`
	ApproveMode    string     `json:"approve_mode"`
	Status         string     `json:"status"`
	WorkerID       string     `json:"worker_id,omitempty"`
	WorkerLabel    string     `json:"worker_label,omitempty"`
	Summary        string     `json:"summary,omitempty"`
	Risk           string     `json:"risk,omitempty"`
	Error          string     `json:"error,omitempty"`
	PlanResultRef  string     `json:"plan_result_ref,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	ClaimedAt      *time.Time `json:"claimed_at,omitempty"`
	LeaseExpiresAt *time.Time `json:"lease_expires_at,omitempty"`
}

// PostRunResultInput is the bridge result payload.
type PostRunResultInput struct {
	Status  string          `json:"status"`
	Summary string          `json:"summary,omitempty"`
	Risk    string          `json:"risk,omitempty"`
	Error   string          `json:"error,omitempty"`
	Body    json.RawMessage `json:"body,omitempty"`
}

// ClaimRun claims the next queued (or expired-lease) job. ok=false means 204 empty.
func (c *Client) ClaimRun(ctx context.Context, workerLabel string) (RunJob, bool, error) {
	body := map[string]string{}
	if strings.TrimSpace(workerLabel) != "" {
		body["worker_label"] = strings.TrimSpace(workerLabel)
	}
	var payload any
	if len(body) > 0 {
		payload = body
	} else {
		payload = map[string]any{}
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return RunJob{}, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/runs/claim", bytes.NewReader(b))
	if err != nil {
		return RunJob{}, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return RunJob{}, false, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return RunJob{}, false, err
	}
	if res.StatusCode == http.StatusNoContent {
		return RunJob{}, false, nil
	}
	if res.StatusCode >= 400 {
		var ae apiError
		_ = json.Unmarshal(data, &ae)
		msg := strings.TrimSpace(ae.Error.Message)
		if msg == "" {
			msg = strings.TrimSpace(string(data))
		}
		if msg == "" {
			msg = res.Status
		}
		return RunJob{}, false, fmt.Errorf("api %s: %s", res.Status, msg)
	}
	var out RunJob
	if err := json.Unmarshal(data, &out); err != nil {
		return RunJob{}, false, err
	}
	return out, true, nil
}

// PostRunResult pushes plan status / PlanResult JSON for a claimed job.
func (c *Client) PostRunResult(ctx context.Context, runID string, in PostRunResultInput) (RunJob, error) {
	var out RunJob
	err := c.doJSON(ctx, http.MethodPost, "/v1/runs/"+runID+"/result", in, c.Token, &out)
	return out, err
}
