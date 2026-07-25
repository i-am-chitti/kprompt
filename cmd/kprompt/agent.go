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
		ns         string
		kubeCtx    string
		inCluster  bool
		emitJSON   bool
		emitInitial bool
		duration   time.Duration
	)
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Watch Pods and Events in a namespace (no LLM)",
		Long: `Start the AG-003 watch engine for one namespace.

Prints Pod and Event changes until interrupted. Observe Mode only — no apply/patch/delete.`,
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
			handler := func(ev agentwatch.Event) {
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

			fmt.Fprintf(cmd.ErrOrStderr(), "kprompt agent watching namespace %q (Pods+Events, read-only)…\n", ns)
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
	cmd.Flags().BoolVar(&emitJSON, "json", false, "emit one JSON Event per line")
	cmd.Flags().BoolVar(&emitInitial, "emit-initial", false, "emit current Pods/Events as Added before live watch")
	cmd.Flags().DurationVar(&duration, "duration", 0, "stop after duration (0 = until signal); useful for e2e")
	_ = cmd.MarkFlagRequired("namespace")
	return cmd
}
