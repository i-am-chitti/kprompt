package webhook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kprompt/kprompt/internal/incident"
)

func TestNotifySuccess(t *testing.T) {
	var got incident.AgentAlert
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type %s", r.Header.Get("Content-Type"))
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("decode: %v", err)
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()

	c := New(Config{URL: srv.URL, HTTPClient: srv.Client()})
	alert := sampleAlert()
	if err := c.Notify(context.Background(), alert); err != nil {
		t.Fatal(err)
	}
	if got.IncidentID != "inc-42" || got.Summary != "CrashLoopBackOff" {
		t.Fatalf("%+v", got)
	}
}

func TestNotifyRetriesThenSucceeds(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) < 3 {
			w.WriteHeader(503)
			_, _ = w.Write([]byte("busy"))
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := New(Config{
		URL:        srv.URL,
		Attempts:   3,
		Backoff:    10 * time.Millisecond,
		HTTPClient: srv.Client(),
	})
	if err := c.Notify(context.Background(), sampleAlert()); err != nil {
		t.Fatal(err)
	}
	if n.Load() != 3 {
		t.Fatalf("attempts=%d", n.Load())
	}
}

func TestNotifyNoRetryOn400(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(400)
		_, _ = w.Write([]byte("bad"))
	}))
	defer srv.Close()

	c := New(Config{
		URL:        srv.URL,
		Attempts:   3,
		Backoff:    time.Millisecond,
		HTTPClient: srv.Client(),
	})
	if err := c.Notify(context.Background(), sampleAlert()); err == nil {
		t.Fatal("expected error")
	}
	if n.Load() != 1 {
		t.Fatalf("expected single attempt, got %d", n.Load())
	}
}

func TestConfigEnabled(t *testing.T) {
	if (Config{}).Enabled() {
		t.Fatal("empty")
	}
	if !(Config{URL: "https://example.com/hook"}).Enabled() {
		t.Fatal("url")
	}
}

func sampleAlert() incident.AgentAlert {
	return incident.NewAgentAlert(incident.Incident{
		ID:         "inc-42",
		Namespace:  "payments",
		Severity:   incident.SeverityHigh,
		Confidence: 0.9,
		Summary:    "CrashLoopBackOff",
		RootCause:  "x",
	}, incident.AlertFired, time.Now().UTC())
}
