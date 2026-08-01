package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRecipeShowCompletion(t *testing.T) {
	cmd := newRecipeShowCmd()
	if cmd.ValidArgsFunction == nil {
		t.Fatal("expected ValidArgsFunction to be set")
	}
	// Call ValidArgsFunction
	completions, directive := cmd.ValidArgsFunction(cmd, []string{}, "")
	if len(completions) == 0 {
		t.Fatal("expected recipe IDs in completion list")
	}
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("expected directive ShellCompDirectiveNoFileComp, got %v", directive)
	}

	// Try with a prefix
	completions, _ = cmd.ValidArgsFunction(cmd, []string{}, "har")
	for _, c := range completions {
		if !strings.HasPrefix(strings.ToLower(c), "har") {
			t.Fatalf("unexpected completion value for prefix 'har': %s", c)
		}
	}

	// Try when arg already exists
	completions, _ = cmd.ValidArgsFunction(cmd, []string{"harden-production"}, "")
	if len(completions) != 0 {
		t.Fatalf("expected no completions if argument is already provided, got %v", completions)
	}
}

func TestRecipeRunCompletion(t *testing.T) {
	cmd := newRecipeRunCmd()
	if cmd.ValidArgsFunction == nil {
		t.Fatal("expected ValidArgsFunction to be set")
	}
	// Call ValidArgsFunction
	completions, directive := cmd.ValidArgsFunction(cmd, []string{}, "")
	if len(completions) == 0 {
		t.Fatal("expected recipe IDs in completion list")
	}
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("expected directive ShellCompDirectiveNoFileComp, got %v", directive)
	}
}
