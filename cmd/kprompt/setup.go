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
		kubeCtx   string
		profile   string
		only      []string
		dryRun    bool
		approve   bool
		jsonOut   bool
		argoNS    string
		promNS    string
		promRel   string
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Detect gaps; dry-run plan or approve host/cluster installs",
		Long: `Builds a bootstrap plan from tools.Detect (ADR-0018).

Default is dry-run. With --approve (or interactive confirm):
  • Host: Helm via brew / get-helm-3
  • Cluster: Argo Workflows + kube-prometheus-stack — plan → safety → apply
Never silent. Wipe-class uninstalls are denied.

` + setup.ProfilesDoc() + `

--only filters the selected profile (comma-separated or repeatable):
  helm · argo-workflows · prometheus · grafana · opentelemetry

` + setup.OSMatrixDoc() + "\n" + setup.NamespaceDefaultsDoc(),
		Example: `  # Detect missing tools and print dry-run installation plan
  kprompt setup

  # Approve setup with minimal profile
  kprompt setup --profile minimal --approve

  # Platform profile, Prometheus only
  kprompt setup --profile platform --only prometheus --approve

  # Approve setup with platform profile on a specific kube context
  kprompt setup --profile platform --approve --context kind-dev

  # Generate and print the setup plan in JSON format
  kprompt setup --dry-run --json`,
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

			// Printed plan is always dry-run; apply is a separate approve/TTY phase.
			plan, err := setup.BuildPlan(reg, setup.Options{
				Profile: profile,
				Only:    only,
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
			clusterSteps := setup.ClusterNeeded(plan)
			if len(hostSteps) == 0 && len(clusterSteps) == 0 {
				if approve {
					fmt.Fprintln(cmd.OutOrStdout(), "\nNothing to install (ready or config-only). Re-check: kprompt tools")
				}
				return nil
			}

			wantApply := approve
			if !wantApply {
				if dryRun && !ui.StdinIsTerminal() {
					fmt.Fprintln(cmd.OutOrStdout(), "\nInstalls available — re-run with --approve, or in a TTY to confirm.")
					return nil
				}
				if !ui.StdinIsTerminal() {
					ui.PrintNeedsApprove(cmd.OutOrStdout())
					return nil
				}
				ok, err := ui.ConfirmSetupApply(os.Stdin, cmd.OutOrStdout())
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

			out := cmd.OutOrStdout()
			if len(hostSteps) > 0 {
				rep, err := setup.ApplyHost(cmd.Context(), plan, setup.DefaultRunner{}, out)
				setup.FormatApply(out, rep)
				if err != nil {
					return err
				}
			}
			if len(clusterSteps) > 0 {
				crep, err := setup.ApplyCluster(cmd.Context(), plan, setup.ClusterApplyOptions{
					KubeContext: ctxName,
					ArgoNS:      argoNS,
					PromNS:      promNS,
					PromRelease: promRel,
					Runner:      setup.DefaultRunner{},
				}, out)
				setup.FormatClusterApply(out, crep)
				if err != nil {
					return err
				}
			}
			fmt.Fprintln(out, "Config-lane steps (Grafana/OTel URLs) stay manual. Re-check: kprompt tools · kprompt doctor")
			return nil
		},
	}

	cmd.Flags().StringVar(&kubeCtx, "context", "", "kubeconfig context for cluster / CRD checks")
	cmd.Flags().StringVar(&profile, "profile", setup.ProfilePlatform, "setup profile: minimal|platform|full")
	cmd.Flags().StringSliceVar(&only, "only", nil, "limit to components (helm,argo-workflows,prometheus,grafana,opentelemetry)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", true, "print plan only (default); apply needs --approve or TTY confirm")
	cmd.Flags().BoolVar(&approve, "approve", false, "apply host + cluster installs from the plan (never silent)")
	cmd.Flags().StringVar(&argoNS, "argo-namespace", setup.DefaultArgoNamespace, "namespace for Argo Workflows install")
	cmd.Flags().StringVar(&promNS, "prometheus-namespace", setup.DefaultPrometheusNamespace, "namespace for kube-prometheus-stack")
	cmd.Flags().StringVar(&promRel, "prometheus-release", setup.DefaultPrometheusRelease, "Helm release name for kube-prometheus-stack")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON plan")
	return cmd
}
