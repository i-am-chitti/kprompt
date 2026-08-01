package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCompletionCmd(t *testing.T) {
	tests := []struct {
		shell string
	}{
		{"bash"},
		{"zsh"},
		{"fish"},
	}

	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			root := &cobra.Command{Use: "kprompt"}
			root.AddCommand(newCompletionCmd())

			buf := new(bytes.Buffer)
			root.SetOut(buf)
			root.SetErr(buf)
			root.SetArgs([]string{"completion", tt.shell})

			if err := root.Execute(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			output := buf.String()
			if len(output) == 0 {
				t.Fatalf("empty output generated for %s", tt.shell)
			}

			// basic sanity checks
			if tt.shell == "bash" && !strings.Contains(output, "complete -o") {
				t.Fatalf("expected bash completion function in output")
			}
			if tt.shell == "zsh" && !strings.Contains(output, "#compdef") {
				t.Fatalf("expected zsh completion function in output")
			}
			if tt.shell == "fish" && !strings.Contains(output, "complete -c") {
				t.Fatalf("expected fish completion function in output")
			}
		})
	}
}

func TestCompletionCmdInvalid(t *testing.T) {
	root := &cobra.Command{Use: "kprompt"}
	root.AddCommand(newCompletionCmd())

	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"completion", "invalid-shell"})

	if err := root.Execute(); err == nil {
		t.Fatal("expected error for invalid shell")
	}
}
