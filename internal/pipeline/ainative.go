package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/kprompt/kprompt/internal/cluster"
	"github.com/kprompt/kprompt/internal/config"
	"github.com/kprompt/kprompt/internal/intent"
	"github.com/kprompt/kprompt/internal/output"
	"github.com/kprompt/kprompt/internal/planner"
	"github.com/kprompt/kprompt/internal/remember"
	"github.com/kprompt/kprompt/internal/safety"
	"github.com/kprompt/kprompt/internal/session"
	"github.com/kprompt/kprompt/internal/watchassist"
)

func runSessionPrompt(ctx context.Context, cfg config.Resolved, out io.Writer, deps Deps) error {
	_ = ctx
	jsonMode := cfg.JSONOutput()
	rep, err := session.Digest(session.Options{Local: true})
	if err != nil {
		return err
	}
	plan := planner.ExecutionPlan{
		Intent:  intent.Intent{Kind: intent.KindUnknown, Raw: cfg.Prompt},
		Summary: rep.Summary,
	}
	doc := output.FromPlan(cfg.Prompt, cfg.Context, plan, safety.Result{Risk: safety.RiskLow}, true)
	raw, _ := json.Marshal(rep)
	doc.Result = raw
	if deps.OnResult != nil {
		deps.OnResult(doc)
	}
	if jsonMode {
		return output.Encode(out, doc)
	}
	fmt.Fprintln(out, rep.Summary)
	for _, h := range rep.Highlights {
		fmt.Fprintf(out, "  - %s\n", h)
	}
	return nil
}

func runRememberPrompt(ctx context.Context, cfg config.Resolved, out io.Writer, deps Deps) error {
	_ = ctx
	jsonMode := cfg.JSONOutput()
	p := strings.TrimSpace(cfg.Prompt)
	lower := strings.ToLower(p)

	if strings.HasPrefix(lower, "forget ") {
		key := strings.TrimSpace(p[len("forget"):])
		n, err := remember.Forget(key, cfg.Namespace)
		if err != nil {
			return err
		}
		msg := fmt.Sprintf("Forgot %d fact(s) matching %q", n, key)
		return emitMemoryNote(cfg, out, deps, jsonMode, msg, map[string]any{"removed": n, "key": key})
	}

	stmt := p
	if strings.HasPrefix(lower, "remember that ") {
		stmt = strings.TrimSpace(p[len("remember that"):])
	} else if strings.HasPrefix(lower, "remember ") {
		stmt = strings.TrimSpace(p[len("remember"):])
	} else {
		return fmt.Errorf("remember: use 'remember key = value' or 'forget key'")
	}
	key, value, err := remember.ParseStatement(stmt)
	if err != nil {
		return err
	}
	ns := cfg.Namespace
	if !cfg.NamespaceFromCLI {
		ns = ""
	}
	f, err := remember.Upsert(key, value, ns)
	if err != nil {
		return err
	}
	msg := fmt.Sprintf("Remembered %s = %s", f.Key, f.Value)
	return emitMemoryNote(cfg, out, deps, jsonMode, msg, f)
}

func emitMemoryNote(cfg config.Resolved, out io.Writer, deps Deps, jsonMode bool, msg string, payload any) error {
	plan := planner.ExecutionPlan{
		Intent:  intent.Intent{Kind: intent.KindUnknown, Raw: cfg.Prompt},
		Summary: msg,
	}
	doc := output.FromPlan(cfg.Prompt, cfg.Context, plan, safety.Result{Risk: safety.RiskLow}, true)
	raw, _ := json.Marshal(payload)
	doc.Result = raw
	if deps.OnResult != nil {
		deps.OnResult(doc)
	}
	if jsonMode {
		return output.Encode(out, doc)
	}
	fmt.Fprintln(out, msg)
	return nil
}

func runWatchOncePrompt(ctx context.Context, cfg config.Resolved, out io.Writer, deps Deps) error {
	jsonMode := cfg.JSONOutput()
	ns := strings.TrimSpace(cfg.Namespace)
	if !cfg.NamespaceFromCLI {
		if phraseNS, _ := intent.ParseScopePhrases(cfg.Prompt); phraseNS != "" {
			ns = phraseNS
		}
	}
	if ns == "" {
		return fmt.Errorf("watch requires a namespace (-n); try: kprompt watch -n <ns> --once")
	}
	client := deps.Client
	if client == nil {
		clients, err := cluster.Connect(cfg.Context)
		if err != nil {
			return err
		}
		client = clients.Clientset
	}
	rep, err := (&watchassist.Analyzer{Client: client}).Run(ctx, watchassist.Request{Namespace: ns})
	if err != nil {
		return cluster.Friendlier(err)
	}
	plan := planner.ExecutionPlan{
		Intent:  intent.Intent{Kind: intent.KindUnknown, Raw: cfg.Prompt},
		Summary: rep.Summary,
	}
	doc := output.FromPlan(cfg.Prompt, cfg.Context, plan, safety.Result{Risk: safety.RiskLow}, true)
	raw, _ := json.Marshal(rep)
	doc.Result = raw
	if deps.OnResult != nil {
		deps.OnResult(doc)
	}
	if jsonMode {
		return output.Encode(out, doc)
	}
	fmt.Fprintln(out, rep.Summary)
	for _, s := range rep.Suggestions {
		fmt.Fprintf(out, "  - [%s] %s → %s\n", s.Severity, s.Title, s.Command)
	}
	return nil
}
