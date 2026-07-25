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
	"github.com/kprompt/kprompt/internal/agent/crdstatus"
	"github.com/kprompt/kprompt/internal/agent/ctxbuild"
	"github.com/kprompt/kprompt/internal/agent/health"
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
		trackHealth   bool
		providerName  string
		modelName     string
		minSeverity   string
		minConfidence float64
		duration      time.Duration
		agentCR       string
		agentCRNS     string
		watchList     []string
	)
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Watch Pods and Events in a namespace (Observe Mode)",
		Long: `Start the Observe watch engine for one namespace.

Watched resources (read-only):
  --watch          comma-separated: pods,events (default) plus deployments,
                   replicasets,statefulsets,jobs,cronjobs,pvc,configmaps,secrets
                   (AG-004). secrets are opt-in and metadata-only (never values).

Pipeline flags (read-only — never mutate workload objects):
  --incidents      correlate problem signals into Incidents (AG-006)
  --fetch-logs     on-demand log tail on CrashLoop/Failed/OOM (AG-005)
  --build-context  assemble AgentContext (AG-007)
  --analyze        LLM/heuristic → gated AgentAlert (AG-008)
  --slack          post gated alerts to Slack threads (AG-009)
  --webhook        POST gated AgentAlert JSON to a URL (AG-010)
  --health         emit namespace health score / risk_increasing (AG-011)
  --agent-cr       patch KpromptAgent.status (AG-013; health + lastAlert)

Slack credentials from env / mounted Secret:
  KPROMPT_SLACK_BOT_TOKEN + KPROMPT_SLACK_CHANNEL  (preferred, threaded)
  KPROMPT_SLACK_WEBHOOK_URL                        (fallback)

Generic webhook:
  KPROMPT_WEBHOOK_URL  or  --webhook-url

KpromptAgent status sync:
  --agent-cr / KPROMPT_AGENT_CR  (+ optional --agent-cr-namespace)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ns = strings.TrimSpace(ns)
			if ns == "" {
				return fmt.Errorf("--namespace is required")
			}
			if notifySlack || notifyWebhook {
				doAnalyze = true
			}
			if trackHealth && !incidents {
				incidents = true
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
			var healthTracker *health.Tracker
			var statusSync *crdstatus.Syncer
			threads := map[string]string{}

			crCfg := crdstatus.FromEnv()
			if n := strings.TrimSpace(agentCR); n != "" {
				crCfg.Name = n
			}
			if n := strings.TrimSpace(agentCRNS); n != "" {
				crCfg.Namespace = n
			}
			if crCfg.Name != "" {
				dyn, derr := cluster.DynamicForConfig(clients.Config)
				if derr != nil {
					return fmt.Errorf("dynamic client for KpromptAgent status: %w", derr)
				}
				statusSync = crdstatus.New(dyn, crCfg)
			}

			if incidents {
				builder = correlate.NewBuilder(correlate.Options{Namespace: ns})
			}
			if fetchLogs {
				fetcher = agentlogs.New(clients.Clientset)
			}
			if buildContext || doAnalyze {
				ctxBuilder = &ctxbuild.Builder{Client: clients.Clientset}
			}
			if trackHealth {
				healthTracker = health.NewTracker(ns, clients.Clientset)
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

			emitHealth := func() {
				if healthTracker == nil || builder == nil {
					return
				}
				snap := healthTracker.Evaluate(cmd.Context(), builder.OpenIncidents())
				if statusSync != nil {
					if err := statusSync.PatchHealth(cmd.Context(), snap); err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "kpromptagent status health: %v\n", err)
					}
				}
				if emitJSON {
					_ = json.NewEncoder(out).Encode(snap)
					return
				}
				fmt.Fprintf(out, "health score=%d/100 trend=%s open=%d ready=%s restarts=%d %s\n",
					snap.Score, snap.Trend, snap.OpenIncidents, snap.PodReady, snap.Restarts, snap.Message)
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
						if statusSync != nil && outcome.PassedGate {
							if err := statusSync.PatchAlert(cmd.Context(), outcome.Alert); err != nil {
								fmt.Fprintf(cmd.ErrOrStderr(), "kpromptagent status alert: %v\n", err)
							}
						}
						if emitJSON {
							_ = json.NewEncoder(out).Encode(outcome)
							emitHealth()
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
						emitHealth()
						return
					}
				}
				if ctxBuilder != nil && analyzer == nil {
					switch ch.Kind {
					case correlate.ChangeOpened, correlate.ChangeUpdated, correlate.ChangeReopened:
						agentCtx := ctxBuilder.Build(cmd.Context(), ch.Incident, ctxbuild.Options{})
						if emitJSON {
							_ = json.NewEncoder(out).Encode(agentCtx)
							emitHealth()
							return
						}
						fmt.Fprintf(out, "context id=%s target=%v degraded=%v\n",
							agentCtx.Incident.ID, agentCtx.Target, agentCtx.Degraded)
						for _, line := range agentCtx.PromptBlocks() {
							fmt.Fprintf(out, "  %s\n", line)
						}
						emitHealth()
						return
					}
				}
				if emitJSON {
					_ = json.NewEncoder(out).Encode(ch)
					emitHealth()
					return
				}
				fmt.Fprintf(out, "incident %s id=%s severity=%s status=%s summary=%s evidence=%d\n",
					ch.Kind, ch.Incident.ID, ch.Incident.Severity, ch.Incident.Status,
					ch.Incident.Summary, len(ch.Incident.Evidence))
				emitHealth()
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
				case agentwatch.ResourcePod:
					fmt.Fprintf(out, "%s %s %s/%s phase=%s\n",
						ev.Type, ev.Resource, ev.Namespace, ev.Name, ev.PodPhase)
				default:
					extra := ev.Detail
					if ev.PodPhase != "" {
						extra = "phase=" + ev.PodPhase + " " + extra
					}
					fmt.Fprintf(out, "%s %s %s/%s %s\n",
						ev.Type, ev.Resource, ev.Namespace, ev.Name, strings.TrimSpace(extra))
				}
			}

			resources := agentwatch.NormalizeResources(watchList)
			eng := &agentwatch.Engine{
				Client: clients.Clientset,
				Options: agentwatch.Options{
					Namespace:   ns,
					Resources:   resources,
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
			case trackHealth:
				mode = "watch → incidents → health"
			case incidents && fetchLogs:
				mode = "Pods+Events → Incidents + on-demand logs"
			case incidents:
				mode = "Pods+Events → Incidents"
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "kprompt agent watching namespace %q resources=%s (%s, read-only)…\n",
				ns, strings.Join(resources, ","), mode)

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
							emitHealth()
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
	cmd.Flags().StringSliceVar(&watchList, "watch", nil, "resources to watch (default pods,events; AG-004: deployments,replicasets,statefulsets,jobs,cronjobs,pvc,configmaps,secrets). secrets are opt-in and metadata-only")
	cmd.Flags().BoolVar(&incidents, "incidents", false, "correlate problem signals into Incident changes (AG-006)")
	cmd.Flags().BoolVar(&fetchLogs, "fetch-logs", false, "on CrashLoop/Failed/OOM attach a short log tail (AG-005; enables --incidents)")
	cmd.Flags().BoolVar(&buildContext, "build-context", false, "assemble AgentContext for LLM (AG-007; enables --incidents)")
	cmd.Flags().BoolVar(&doAnalyze, "analyze", false, "run LLM/heuristic analyzer → gated AgentAlert (AG-008)")
	cmd.Flags().BoolVar(&notifySlack, "slack", false, "post gated alerts to Slack (AG-009; enables --analyze)")
	cmd.Flags().BoolVar(&notifyWebhook, "webhook", false, "POST gated AgentAlert JSON to webhook URL (AG-010; enables --analyze)")
	cmd.Flags().StringVar(&webhookURL, "webhook-url", "", "override KPROMPT_WEBHOOK_URL for --webhook")
	cmd.Flags().BoolVar(&trackHealth, "health", false, "emit namespace health score and risk_increasing trends (AG-011; enables --incidents)")
	cmd.Flags().StringVar(&agentCR, "agent-cr", "", "KpromptAgent name to patch status (AG-013; or KPROMPT_AGENT_CR)")
	cmd.Flags().StringVar(&agentCRNS, "agent-cr-namespace", "", "namespace of --agent-cr (default: POD_NAMESPACE / default)")
	cmd.Flags().BoolVar(&heuristic, "heuristic", false, "with --analyze, skip LLM and use local heuristics only")
	cmd.Flags().StringVar(&providerName, "provider", "", "LLM provider for --analyze (default from config)")
	cmd.Flags().StringVar(&modelName, "model", "", "LLM model for --analyze (default from config)")
	cmd.Flags().StringVar(&minSeverity, "min-severity", "", "alert gate minimum severity (default medium)")
	cmd.Flags().Float64Var(&minConfidence, "min-confidence", 0, "alert gate minimum confidence 0..1 (default 0.7)")
	cmd.Flags().DurationVar(&duration, "duration", 0, "stop after duration (0 = until signal); useful for e2e")
	_ = cmd.MarkFlagRequired("namespace")
	return cmd
}
