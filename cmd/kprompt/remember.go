package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/kprompt/kprompt/internal/remember"
)

func newRememberCmd() *cobra.Command {
	var (
		ns      string
		jsonOut bool
	)
	cmd := &cobra.Command{
		Use:   "remember [statement]",
		Short: "Store a local operator fact (never uploaded by default)",
		Long: `Durable local memory (S-015 · ADR-0022) in ~/.kprompt/memory.json.

Examples of statements: "payment ns = Team A", "tier: gold".
Facts bias later planning hints only — live cluster reads still win.
Privacy: local-only; not synced to api.kprompt.ai in MVP.`,
		Example: `  kprompt remember "payment ns = Team A"
  kprompt remember "oncall = alice" -n payments
  kprompt remember list
  kprompt forget "payment ns"`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return listRemember(cmd, ns, jsonOut)
			}
			stmt := strings.Join(args, " ")
			key, value, err := remember.ParseStatement(stmt)
			if err != nil {
				return err
			}
			if ns == "" {
				ns = namespace
			}
			f, err := remember.Upsert(key, value, ns)
			if err != nil {
				return err
			}
			path, _ := remember.DefaultPath()
			fmt.Fprintf(cmd.OutOrStdout(), "Remembered %s = %s", f.Key, f.Value)
			if f.Namespace != "" {
				fmt.Fprintf(cmd.OutOrStdout(), " (-n %s)", f.Namespace)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "\nFile: %s\n", path)
			return nil
		},
	}
	cmd.Flags().StringVarP(&ns, "namespace", "n", "", "optional namespace scope for the fact")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "JSON output for list")
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List local memory facts",
		RunE: func(cmd *cobra.Command, args []string) error {
			return listRemember(cmd, ns, jsonOut)
		},
	})
	return cmd
}

func newForgetCmd() *cobra.Command {
	var ns string
	cmd := &cobra.Command{
		Use:   "forget <key>",
		Short: "Remove a local remember fact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if ns == "" {
				ns = namespace
			}
			n, err := remember.Forget(args[0], ns)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Forgot %d fact(s) matching %q\n", n, args[0])
			return nil
		},
	}
	cmd.Flags().StringVarP(&ns, "namespace", "n", "", "only remove facts in this namespace")
	return cmd
}

func listRemember(cmd *cobra.Command, ns string, jsonOut bool) error {
	if ns == "" {
		ns = namespace
	}
	facts, err := remember.List(ns)
	if err != nil {
		return err
	}
	if jsonOut || outputFmt == "json" {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(facts)
	}
	if len(facts) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No local memory facts.")
		return nil
	}
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAMESPACE\tKEY\tVALUE")
	for _, f := range facts {
		nsLabel := f.Namespace
		if nsLabel == "" {
			nsLabel = "*"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", nsLabel, f.Key, f.Value)
	}
	_ = tw.Flush()
	path, _ := remember.DefaultPath()
	fmt.Fprintf(cmd.OutOrStdout(), "File: %s\n", path)
	return nil
}
