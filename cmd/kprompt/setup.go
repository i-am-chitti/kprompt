package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kprompt/kprompt/internal/config"
	"github.com/kprompt/kprompt/internal/setup"
	"github.com/kprompt/kprompt/internal/tools"
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
		Short: "Detect gaps and print a bootstrap plan (dry-run)",
		Long: `Builds an install/configure plan from tools.Detect (ADR-0018 · T-062).

Default is dry-run: prints host / cluster / config steps for the selected profile.
Never installs silently. Host apply = T-063 · cluster apply = T-064.

Profiles:
  minimal   Helm CLI only
  platform  Helm + Argo Workflows + Prometheus (default)
  full      platform + Grafana + OpenTelemetry URL config

  kprompt setup
  kprompt setup --profile minimal
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

			if approve {
				return fmt.Errorf("setup --approve apply is not implemented yet (host T-063 / cluster T-064); plan printed above is dry-run only — nothing was installed")
			}
			_ = dryRun // default true; flag kept for UX honesty / future apply gate
			return nil
		},
	}

	cmd.Flags().StringVar(&kubeCtx, "context", "", "kubeconfig context for cluster / CRD checks")
	cmd.Flags().StringVar(&profile, "profile", setup.ProfilePlatform, "setup profile: minimal|platform|full")
	cmd.Flags().BoolVar(&dryRun, "dry-run", true, "print plan only (default; mutations require later T-063/T-064)")
	cmd.Flags().BoolVar(&approve, "approve", false, "reserved for apply (T-063/T-064) — errors in T-062")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON plan")
	return cmd
}
