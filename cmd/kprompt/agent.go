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

	"github.com/kprompt/kprompt/internal/agent/correlate"
	agentwatch "github.com/kprompt/kprompt/internal/agent/watch"
	"github.com/kprompt/kprompt/internal/cluster"
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
		ns          string
		kubeCtx     string
		inCluster   bool
		emitJSON    bool
		emitInitial bool
		incidents   bool
		duration    time.Duration
	)
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Watch Pods and Events in a namespace (no LLM)",
		Long: `Start the Observe watch engine for one namespace.

Prints Pod and Event changes until interrupted. With --incidents, correlates
problem signals into Incident objects (AG-006). Observe Mode only — no apply/patch/delete.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ns = strings.TrimSpace(ns)
			if ns == "" {
				return fmt.Errorf("--namespace is required")
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
			if incidents {
				builder = correlate.NewBuilder(correlate.Options{Namespace: ns})
			}

			handler := func(ev agentwatch.Event) {
				if builder != nil {
					if ch, ok := builder.Ingest(ev); ok {
						if emitJSON {
							_ = json.NewEncoder(out).Encode(ch)
						} else {
							fmt.Fprintf(out, "incident %s id=%s severity=%s status=%s summary=%s evidence=%d\n",
								ch.Kind, ch.Incident.ID, ch.Incident.Severity, ch.Incident.Status,
								ch.Incident.Summary, len(ch.Incident.Evidence))
						}
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
			if incidents {
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
								if emitJSON {
									_ = json.NewEncoder(out).Encode(ch)
								} else {
									fmt.Fprintf(out, "incident %s id=%s status=%s summary=%s\n",
										ch.Kind, ch.Incident.ID, ch.Incident.Status, ch.Incident.Summary)
								}
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
	cmd.Flags().DurationVar(&duration, "duration", 0, "stop after duration (0 = until signal); useful for e2e")
	_ = cmd.MarkFlagRequired("namespace")
	return cmd
}
