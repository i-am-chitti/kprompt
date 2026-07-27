package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/kprompt/kprompt/internal/config"
	"github.com/kprompt/kprompt/internal/setup"
	"github.com/kprompt/kprompt/internal/tools"
	"github.com/kprompt/kprompt/internal/ui"
)

func newSetupCmd() *cobra.Command {
	var (
		kubeCtx string
		profile string
		dryRun  bool
		approve bool
		jsonOut bool
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Detect gaps; dry-run plan or approve host CLI installs",
		Long: `Builds a bootstrap plan from tools.Detect (ADR-0018).

Default is dry-run. With --approve (or interactive confirm), installs missing
host CLIs where safe (T-063: Helm via brew / get-helm-3). Cluster operators
and URL config are still plan-only (T-064).

Profiles:
  minimal   Helm CLI only
  platform  Helm + Argo Workflows + Prometheus (default)
  full      platform + Grafana + OpenTelemetry URL config

` + setup.OSMatrixDoc() + `
  kprompt setup
  kprompt setup --profile minimal --approve
  kprompt setup --dry-run --json
  kprompt setup --profile platform --context kind-dev`,
		RunE: func(cmd *cobra.Command, args []string) error {
			file, err := config.LoadFile()
			if err != nil {
				return err
			}
			ctxName := kubeCtx
			if ctxName == "" {
				ctxName = file.Context
			}
			reg, err := tools.Detect(cmd.Context(), tools.DetectOptions{
				Context: ctxName,
				File:    file,
			})
			if err != nil {
				return err
			}
			plan, err := setup.BuildPlan(reg, setup.Options{
				Profile: profile,
				DryRun:  true,
			})
			if err != nil {
				return err
			}

			if jsonOut {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if err := enc.Encode(plan); err != nil {
					return err
				}
			} else {
				if err := setup.FormatText(cmd.OutOrStdout(), plan); err != nil {
					return err
				}
			}

			hostSteps := setup.HostNeeded(plan)
			if len(hostSteps) == 0 {
				if approve {
					fmt.Fprintln(cmd.OutOrStdout(), "\nNo host CLIs to install (skip or already on PATH). Cluster/config steps stay plan-only (T-064).")
				}
				return nil
			}

			// Dry-run only unless approve or interactive confirm.
			wantApply := approve
			if !wantApply {
				if dryRun && !ui.StdinIsTerminal() {
					fmt.Fprintln(cmd.OutOrStdout(), "\nHost installs available — re-run with --approve, or in a TTY to confirm.")
					return nil
				}
				if !ui.StdinIsTerminal() {
					ui.PrintNeedsApprove(cmd.OutOrStdout())
					return nil
				}
				ok, err := ui.ConfirmHostInstall(os.Stdin, cmd.OutOrStdout())
				if err != nil {
					return err
				}
				if !ok {
					ui.PrintAborted(cmd.OutOrStdout())
					return nil
				}
				wantApply = true
			}
			if !wantApply {
				return nil
			}

			rep, err := setup.ApplyHost(cmd.Context(), plan, setup.DefaultRunner{}, cmd.OutOrStdout())
			setup.FormatApply(cmd.OutOrStdout(), rep)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Cluster/config steps were not applied (T-064 / manual config). Re-check: kprompt tools")
			return nil
		},
	}

	cmd.Flags().StringVar(&kubeCtx, "context", "", "kubeconfig context for cluster / CRD checks")
	cmd.Flags().StringVar(&profile, "profile", setup.ProfilePlatform, "setup profile: minimal|platform|full")
	cmd.Flags().BoolVar(&dryRun, "dry-run", true, "print plan only (default); host apply needs --approve or TTY confirm")
	cmd.Flags().BoolVar(&approve, "approve", false, "install missing host CLIs from the plan (T-063; never silent)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON plan")
	return cmd
}
