package session

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/kprompt/kprompt/internal/history"
)

func TestDigestToday(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	now := time.Date(2026, 8, 2, 15, 0, 0, 0, time.UTC)
	_ = history.AppendPath(path, history.Entry{
		Time: now.Add(-1 * time.Hour), Kind: "investigate", Summary: "rca api", Prompt: "investigate api",
	})
	_ = history.AppendPath(path, history.Entry{
		Time: now.Add(-2 * time.Hour), Kind: "scale", Summary: "scale api", Applied: true, Prompt: "scale api to 3",
	})
	_ = history.AppendPath(path, history.Entry{
		Time: now.Add(-48 * time.Hour), Kind: "deploy", Summary: "old", Prompt: "deploy redis",
	})

	got, err := Digest(Options{Now: now, Path: path, Local: false})
	if err != nil {
		t.Fatal(err)
	}
	if got.Day != "2026-08-02" {
		t.Fatalf("day=%s", got.Day)
	}
	if len(got.Entries) != 2 {
		t.Fatalf("entries=%d", len(got.Entries))
	}
	if got.Counts["investigate"] != 1 || got.Counts["scale"] != 1 {
		t.Fatalf("counts=%v", got.Counts)
	}
	if len(got.Highlights) == 0 {
		t.Fatal("expected highlights")
	}
}
