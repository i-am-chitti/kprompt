package team

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClaimRunNoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/runs/claim" || r.Method != http.MethodPost {
			t.Fatalf("%s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "kp_test")
	_, ok, err := c.ClaimRun(context.Background(), "w1")
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestClaimAndPostRunResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/runs/claim":
			_ = json.NewEncoder(w).Encode(RunJob{
				ID: "run_1", Prompt: "get pods", ApproveMode: "plan_only", Status: "claimed",
			})
		case r.URL.Path == "/v1/runs/run_1/result":
			var in PostRunResultInput
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in.Status != "succeeded" {
				t.Fatalf("%+v", in)
			}
			_ = json.NewEncoder(w).Encode(RunJob{ID: "run_1", Status: in.Status})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "kp_test")
	job, ok, err := c.ClaimRun(context.Background(), "laptop")
	if err != nil || !ok || job.ID != "run_1" {
		t.Fatalf("%+v ok=%v err=%v", job, ok, err)
	}
	out, err := c.PostRunResult(context.Background(), job.ID, PostRunResultInput{
		Status: "succeeded", Summary: "ok", Body: json.RawMessage(`{"kind":"PlanResult"}`),
	})
	if err != nil || out.Status != "succeeded" {
		t.Fatalf("%+v %v", out, err)
	}
}

func TestListenProcessesOneJob(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	claimed := false
	posted := make(chan PostRunResultInput, 2)
	err := Listen(ctx, NewClient("http://example.invalid", "t"), BridgeOptions{
		Interval: time.Millisecond,
		Sleep:    func(time.Duration) {},
		Stdout:   func(string) {},
		PullPolicy: func(context.Context) error { return nil },
		Claim: func(context.Context, string) (RunJob, bool, error) {
			if claimed {
				cancel()
				return RunJob{}, false, nil
			}
			claimed = true
			return RunJob{ID: "run_x", Prompt: "list ns", ApproveMode: "plan_only"}, true, nil
		},
		Execute: func(context.Context, RunJob) (PostRunResultInput, error) {
			return PostRunResultInput{Status: "succeeded", Summary: "ok"}, nil
		},
		Post: func(_ context.Context, id string, in PostRunResultInput) (RunJob, error) {
			posted <- in
			return RunJob{ID: id, Status: in.Status}, nil
		},
	})
	if err != nil && err != context.Canceled {
		t.Fatal(err)
	}
	var sawSuccess bool
	for i := 0; i < 2; i++ {
		select {
		case in := <-posted:
			if in.Status == "succeeded" {
				sawSuccess = true
			}
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}
	}
	if !sawSuccess {
		t.Fatal("missing success post")
	}
}
