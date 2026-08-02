package main

import (
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/kprompt/kprompt/internal/session"
)

func newSessionCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Summarize today's local history (day digest)",
		Long: `Session day digest (S-016 · ADR-0022) from ~/.kprompt/history.jsonl only.

Counts investigates / scales / rollbacks / other kinds from today. No cloud
history and no LLM essay in MVP.`,
		Example: `  kprompt session
  kprompt session --json
  kprompt "what did I do today"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			rep, err := session.Digest(session.Options{Local: true})
			if err != nil {
				return err
			}
			if jsonOut || outputFmt == "json" {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(rep)
			}
			fmt.Fprintln(cmd.OutOrStdout(), rep.Summary)
			if len(rep.Highlights) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "Highlights:")
				for _, h := range rep.Highlights {
					fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", h)
				}
			}
			if len(rep.Entries) > 0 {
				tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
				fmt.Fprintln(tw, "TIME\tKIND\tAPPLIED\tPROMPT")
				for _, e := range rep.Entries {
					applied := "no"
					if e.Applied {
						applied = "yes"
					}
					prompt := e.Prompt
					if len(prompt) > 60 {
						prompt = prompt[:57] + "..."
					}
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
						e.Time.Local().Format("15:04"), e.Kind, applied, prompt)
				}
				_ = tw.Flush()
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON SessionDigest")
	return cmd
}
