package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/kprompt/kprompt/internal/agent/analyze"
	"github.com/kprompt/kprompt/internal/agent/correlate"
	"github.com/kprompt/kprompt/internal/agent/ctxbuild"
	agentlogs "github.com/kprompt/kprompt/internal/agent/logs"
	agentslack "github.com/kprompt/kprompt/internal/agent/notify/slack"
	agentwebhook "github.com/kprompt/kprompt/internal/agent/notify/webhook"
	agentwatch "github.com/kprompt/kprompt/internal/agent/watch"
	"github.com/kprompt/kprompt/internal/cluster"
	"github.com/kprompt/kprompt/internal/config"
	"github.com/kprompt/kprompt/internal/llm"
)

func newAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "In-cluster / local Observe agent (read-only)",
		Long:  "Namespace-scoped watch helpers for kprompt Observe Mode. Never mutates the cluster.",
	}
	cmd.AddCommand(newAgentRunCmd())
	return cmd
}

func newAgentRunCmd() *cobra.Command {
	var (
		ns            string
		kubeCtx       string
		inCluster     bool
		emitJSON      bool
		emitInitial   bool
		incidents     bool
		fetchLogs     bool
		buildContext  bool
		doAnalyze     bool
		heuristic     bool
		notifySlack   bool
		notifyWebhook bool
		webhookURL    string
		providerName  string
		modelName     string
		minSeverity   string
		minConfidence float64
		duration      time.Duration
	)
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Watch Pods and Events in a namespace (Observe Mode)",
		Long: `Start the Observe watch engine for one namespace.

Pipeline flags (read-only — never mutate):
  --incidents      correlate problem signals into Incidents (AG-006)
  --fetch-logs     on-demand log tail on CrashLoop/Failed/OOM (AG-005)
  --build-context  assemble AgentContext (AG-007)
  --analyze        LLM/heuristic → gated AgentAlert (AG-008)
  --slack          post gated alerts to Slack threads (AG-009)
  --webhook        POST gated AgentAlert JSON to a URL (AG-010)

Slack credentials from env / mounted Secret:
  KPROMPT_SLACK_BOT_TOKEN + KPROMPT_SLACK_CHANNEL  (preferred, threaded)
  KPROMPT_SLACK_WEBHOOK_URL                        (fallback)

Generic webhook:
  KPROMPT_WEBHOOK_URL  or  --webhook-url`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ns = strings.TrimSpace(ns)
			if ns == "" {
				return fmt.Errorf("--namespace is required")
			}
			if notifySlack || notifyWebhook {
				doAnalyze = true
			}
			if doAnalyze {
				buildContext = true
			}
			if (fetchLogs || buildContext || doAnalyze) && !incidents {
				incidents = true
			}

			var clients *cluster.Clients
			var err error
			if inCluster {
				clients, err = cluster.ConnectInCluster()
			} else {
				clients, err = cluster.Connect(kubeCtx)
			}
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			var builder *correlate.Builder
			var fetcher *agentlogs.Fetcher
			var ctxBuilder *ctxbuild.Builder
			var analyzer *analyze.Analyzer
			var slackClient *agentslack.Client
			var webhookClient *agentwebhook.Client
			threads := map[string]string{}

			if incidents {
				builder = correlate.NewBuilder(correlate.Options{Namespace: ns})
			}
			if fetchLogs {
				fetcher = agentlogs.New(clients.Clientset)
			}
			if buildContext || doAnalyze {
				ctxBuilder = &ctxbuild.Builder{Client: clients.Clientset}
			}
			if doAnalyze {
				opts := analyze.Options{
					MinSeverity:   minSeverity,
					MinConfidence: minConfidence,
					HeuristicOnly: heuristic,
				}
				var provider llm.Provider
				if !heuristic {
					file, err := config.LoadFile()
					if err != nil {
						return err
					}
					cfg := config.Merge(file, providerName, modelName, "", ns, false, "")
					provider, err = llm.New(cfg.Provider, config.APIKeyFor(cfg.Provider), cfg.BaseURL, cfg.Model)
					if err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "warning: LLM unavailable (%v); using heuristic analyzer\n", err)
						opts.HeuristicOnly = true
					}
				}
				analyzer = analyze.New(provider, opts)
			}
			if notifySlack {
				scfg := agentslack.ConfigFromEnv()
				if !scfg.Enabled() {
					return fmt.Errorf("--slack requires %s or %s+%s in the environment (mount from Secret)",
						agentslack.EnvWebhookURL, agentslack.EnvBotToken, agentslack.EnvChannel)
				}
				slackClient = agentslack.New(scfg)
				if !scfg.Threaded() {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: Slack webhook mode cannot reliably thread; prefer bot token + channel\n")
				}
			}
			if notifyWebhook {
				wcfg := agentwebhook.ConfigFromEnv()
				if u := strings.TrimSpace(webhookURL); u != "" {
					wcfg.URL = u
				}
				if !wcfg.Enabled() {
					return fmt.Errorf("--webhook requires %s or --webhook-url", agentwebhook.EnvURL)
				}
				webhookClient = agentwebhook.New(wcfg)
			}

			emitChange := func(ch correlate.Change) {
				if analyzer != nil && ctxBuilder != nil {
					switch ch.Kind {
					case correlate.ChangeOpened, correlate.ChangeUpdated, correlate.ChangeReopened, correlate.ChangeClosed:
						agentCtx := ctxBuilder.Build(cmd.Context(), ch.Incident, ctxbuild.Options{})
						outcome, err := analyzer.Analyze(cmd.Context(), agentCtx, analyze.AlertStatusFor(ch.Kind))
						if err != nil {
							fmt.Fprintf(cmd.ErrOrStderr(), "analyze error: %v\n", err)
							return
						}
						if outcome.Skipped {
							return
						}
						if slackClient != nil && outcome.PassedGate {
							thread := threads[outcome.Alert.IncidentID]
							if thread == "" && builder != nil {
								thread = builder.NotifierThread(outcome.Alert.IncidentID)
							}
							if thread == "" {
								thread = ch.Incident.NotifierThread
							}
							res, err := slackClient.Notify(cmd.Context(), outcome.Alert, thread)
							if err != nil {
								fmt.Fprintf(cmd.ErrOrStderr(), "slack notify error: %v\n", err)
							} else if res.ThreadTS != "" {
								threads[outcome.Alert.IncidentID] = res.ThreadTS
								if builder != nil {
									_ = builder.SetNotifierThread(outcome.Alert.IncidentID, res.ThreadTS)
								}
								ch.Incident.NotifierThread = res.ThreadTS
							}
						}
						if webhookClient != nil && outcome.PassedGate {
							if err := webhookClient.Notify(cmd.Context(), outcome.Alert); err != nil {
								fmt.Fprintf(cmd.ErrOrStderr(), "webhook notify error: %v\n", err)
							}
						}
						if emitJSON {
							_ = json.NewEncoder(out).Encode(outcome)
							return
						}
						gate := "held"
						if outcome.PassedGate {
							gate = "alert"
						}
						extra := ""
						if ts := threads[outcome.Alert.IncidentID]; ts != "" {
							extra = " thread=" + ts
						}
						fmt.Fprintf(out, "%s [%s/%s] id=%s severity=%s conf=%.2f summary=%s rootCause=%s%s\n",
							gate, outcome.Source, outcome.Alert.Status, outcome.Alert.IncidentID,
							outcome.Alert.Severity, outcome.Alert.Confidence, outcome.Alert.Summary, outcome.Alert.RootCause, extra)
						return
					}
				}
				if ctxBuilder != nil && analyzer == nil {
					switch ch.Kind {
					case correlate.ChangeOpened, correlate.ChangeUpdated, correlate.ChangeReopened:
						agentCtx := ctxBuilder.Build(cmd.Context(), ch.Incident, ctxbuild.Options{})
						if emitJSON {
							_ = json.NewEncoder(out).Encode(agentCtx)
							return
						}
						fmt.Fprintf(out, "context id=%s target=%v degraded=%v\n",
							agentCtx.Incident.ID, agentCtx.Target, agentCtx.Degraded)
						for _, line := range agentCtx.PromptBlocks() {
							fmt.Fprintf(out, "  %s\n", line)
						}
						return
					}
				}
				if emitJSON {
					_ = json.NewEncoder(out).Encode(ch)
					return
				}
				fmt.Fprintf(out, "incident %s id=%s severity=%s status=%s summary=%s evidence=%d\n",
					ch.Kind, ch.Incident.ID, ch.Incident.Severity, ch.Incident.Status,
					ch.Incident.Summary, len(ch.Incident.Evidence))
			}

			handler := func(ev agentwatch.Event) {
				if builder != nil {
					if ch, ok := builder.Ingest(ev); ok {
						if fetcher != nil {
							switch ch.Kind {
							case correlate.ChangeOpened, correlate.ChangeUpdated, correlate.ChangeReopened:
								inc := ch.Incident
								fetcher.Attach(cmd.Context(), &inc, ev)
								if snap, synced := builder.SyncIncident(inc); synced {
									ch.Incident = snap
								} else {
									ch.Incident = inc
								}
							}
						}
						emitChange(ch)
					}
					return
				}
				if emitJSON {
					_ = json.NewEncoder(out).Encode(ev)
					return
				}
				switch ev.Resource {
				case agentwatch.ResourceEvent:
					fmt.Fprintf(out, "%s Event %s/%s reason=%s involved=%s/%s %s\n",
						ev.Type, ev.Namespace, ev.Name, ev.Reason, ev.InvolvedKind, ev.InvolvedName, ev.Message)
				default:
					fmt.Fprintf(out, "%s %s %s/%s phase=%s\n",
						ev.Type, ev.Resource, ev.Namespace, ev.Name, ev.PodPhase)
				}
			}

			eng := &agentwatch.Engine{
				Client: clients.Clientset,
				Options: agentwatch.Options{
					Namespace:   ns,
					EmitInitial: emitInitial,
				},
				Handler: handler,
			}

			runCtx := cmd.Context()
			if duration > 0 {
				var cancel context.CancelFunc
				runCtx, cancel = context.WithTimeout(runCtx, duration)
				defer cancel()
			} else {
				var stop context.CancelFunc
				runCtx, stop = signal.NotifyContext(runCtx, os.Interrupt, syscall.SIGTERM)
				defer stop()
			}

			mode := "Pods+Events"
			switch {
			case notifySlack || notifyWebhook:
				mode = "watch → analyze → notify"
			case doAnalyze:
				mode = "watch → incidents → context → analyze"
			case buildContext:
				mode = "watch → incidents → context"
			case incidents && fetchLogs:
				mode = "Pods+Events → Incidents + on-demand logs"
			case incidents:
				mode = "Pods+Events → Incidents"
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "kprompt agent watching namespace %q (%s, read-only)…\n", ns, mode)

			if builder != nil {
				go func() {
					t := time.NewTicker(30 * time.Second)
					defer t.Stop()
					for {
						select {
						case <-runCtx.Done():
							return
						case <-t.C:
							for _, ch := range builder.Sweep() {
								emitChange(ch)
							}
						}
					}
				}()
			}

			err = eng.Run(runCtx)
			if err == context.Canceled || err == context.DeadlineExceeded {
				return nil
			}
			return err
		},
	}
	cmd.Flags().StringVarP(&ns, "namespace", "n", "", "namespace to watch (required)")
	cmd.Flags().StringVar(&kubeCtx, "context", "", "kubeconfig context (ignored with --in-cluster)")
	cmd.Flags().BoolVar(&inCluster, "in-cluster", false, "use InClusterConfig (ServiceAccount)")
	cmd.Flags().BoolVar(&emitJSON, "json", false, "emit one JSON object per line")
	cmd.Flags().BoolVar(&emitInitial, "emit-initial", false, "emit current Pods/Events as Added before live watch")
	cmd.Flags().BoolVar(&incidents, "incidents", false, "correlate problem signals into Incident changes (AG-006)")
	cmd.Flags().BoolVar(&fetchLogs, "fetch-logs", false, "on CrashLoop/Failed/OOM attach a short log tail (AG-005; enables --incidents)")
	cmd.Flags().BoolVar(&buildContext, "build-context", false, "assemble AgentContext for LLM (AG-007; enables --incidents)")
	cmd.Flags().BoolVar(&doAnalyze, "analyze", false, "run LLM/heuristic analyzer → gated AgentAlert (AG-008)")
	cmd.Flags().BoolVar(&notifySlack, "slack", false, "post gated alerts to Slack (AG-009; enables --analyze)")
	cmd.Flags().BoolVar(&notifyWebhook, "webhook", false, "POST gated AgentAlert JSON to webhook URL (AG-010; enables --analyze)")
	cmd.Flags().StringVar(&webhookURL, "webhook-url", "", "override KPROMPT_WEBHOOK_URL for --webhook")
	cmd.Flags().BoolVar(&heuristic, "heuristic", false, "with --analyze, skip LLM and use local heuristics only")
	cmd.Flags().StringVar(&providerName, "provider", "", "LLM provider for --analyze (default from config)")
	cmd.Flags().StringVar(&modelName, "model", "", "LLM model for --analyze (default from config)")
	cmd.Flags().StringVar(&minSeverity, "min-severity", "", "alert gate minimum severity (default medium)")
	cmd.Flags().Float64Var(&minConfidence, "min-confidence", 0, "alert gate minimum confidence 0..1 (default 0.7)")
	cmd.Flags().DurationVar(&duration, "duration", 0, "stop after duration (0 = until signal); useful for e2e")
	_ = cmd.MarkFlagRequired("namespace")
	return cmd
}
