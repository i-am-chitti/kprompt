package ask

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	agentslack "github.com/kprompt/kprompt/internal/agent/notify/slack"
)

// ListenConfig configures the Slack Events API HTTP listener (AG-019).
type ListenConfig struct {
	Addr   string // e.g. ":8080"
	Path   string // default /slack/events
	Client *agentslack.Client
	Ask    *Handler
	Logf   func(string, ...any)
}

// ListenAndServe starts an HTTP server for Slack Events (url_verification + app_mention / message).
// Blocks until ctx is cancelled. Requires a public URL or port-forward to receive Events.
func ListenAndServe(ctx context.Context, cfg ListenConfig) error {
	if cfg.Ask == nil || cfg.Client == nil {
		return fmt.Errorf("ask: client and handler required")
	}
	path := cfg.Path
	if path == "" {
		path = "/slack/events"
	}
	addr := cfg.Addr
	if addr == "" {
		addr = ":8080"
	}
	logf := cfg.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}

	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		var envelope struct {
			Type      string `json:"type"`
			Challenge  string `json:"challenge"`
			Event     struct {
				Type    string `json:"type"`
				Text    string `json:"text"`
				User    string `json:"user"`
				Channel string `json:"channel"`
				TS      string `json:"ts"`
				Thread  string `json:"thread_ts"`
				BotID   string `json:"bot_id"`
			} `json:"event"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if envelope.Type == "url_verification" {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(envelope.Challenge))
			return
		}
		w.WriteHeader(http.StatusOK)
		if envelope.Type != "event_callback" {
			return
		}
		ev := envelope.Event
		if ev.BotID != "" {
			return // ignore bot echoes
		}
		if ev.Type != "app_mention" && ev.Type != "message" {
			return
		}
		// Only handle messages in the configured channel when set.
		if ch := cfg.Client.Channel(); ch != "" && ev.Channel != "" &&
			!strings.EqualFold(ch, ev.Channel) && !strings.HasPrefix(ch, "#") {
			// allow #name vs id mismatch — still answer app_mention
			if ev.Type == "message" {
				return
			}
		}
		reply := cfg.Ask.Answer(r.Context(), ev.Text)
		thread := ev.Thread
		if thread == "" {
			thread = ev.TS
		}
		go func() {
			cctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if _, err := cfg.Client.PostText(cctx, reply, thread); err != nil {
				logf("slack ask reply: %v", err)
			}
		}()
	})

	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	logf("slack ask listening on %s%s", addr, path)
	err := srv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
