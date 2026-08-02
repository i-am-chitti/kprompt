// Package session builds a day digest from local history (S-016 · ADR-0022 · T-019).
package session

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kprompt/kprompt/internal/history"
)

const TypeSessionDigest = "SessionDigest"

// Report is today's history rollup.
type Report struct {
	Type       string          `json:"type"`
	Day        string          `json:"day"` // YYYY-MM-DD (local or UTC label)
	Summary    string          `json:"summary"`
	Counts     map[string]int  `json:"counts"`
	Entries    []history.Entry `json:"entries"`
	Highlights []string        `json:"highlights,omitempty"`
}

// Options configures Digest.
type Options struct {
	Now   time.Time // injectable clock
	Limit int       // max history rows to scan (default 200)
	Path  string    // optional history path (tests)
	Local bool      // use local timezone for "today" (default true)
}

// Digest summarizes history entries from the start of today.
func Digest(opts Options) (Report, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	loc := time.UTC
	if opts.Local {
		loc = time.Local
	}
	now = now.In(loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	limit := opts.Limit
	if limit <= 0 {
		limit = 200
	}

	var (
		entries []history.Entry
		err     error
	)
	if opts.Path != "" {
		entries, err = history.ListPath(opts.Path, limit)
	} else {
		entries, err = history.List(limit)
	}
	if err != nil {
		return Report{}, err
	}

	var today []history.Entry
	counts := map[string]int{}
	for _, e := range entries {
		t := e.Time.In(loc)
		if t.Before(start) {
			continue
		}
		today = append(today, e)
		k := e.Kind
		if k == "" {
			k = "unknown"
		}
		counts[k]++
	}

	// Keep chronological for digest (oldest first within the day).
	sort.Slice(today, func(i, j int) bool {
		return today[i].Time.Before(today[j].Time)
	})

	day := start.Format("2006-01-02")
	rep := Report{
		Type:    TypeSessionDigest,
		Day:     day,
		Counts:  counts,
		Entries: today,
	}
	if len(today) == 0 {
		rep.Summary = fmt.Sprintf("No local history entries for %s", day)
		return rep, nil
	}
	rep.Summary = fmt.Sprintf(
		"%d action(s) on %s from local history (%s)",
		len(today), day, formatCounts(counts),
	)
	rep.Highlights = highlights(today)
	return rep, nil
}

func formatCounts(counts map[string]int) string {
	type kv struct {
		k string
		n int
	}
	var items []kv
	for k, n := range counts {
		items = append(items, kv{k, n})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].n != items[j].n {
			return items[i].n > items[j].n
		}
		return items[i].k < items[j].k
	})
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, fmt.Sprintf("%s×%d", it.k, it.n))
	}
	return strings.Join(parts, ", ")
}

func highlights(entries []history.Entry) []string {
	var out []string
	interesting := map[string]bool{
		"investigate": true, "why": true, "scale": true, "rollback": true,
		"deploy": true, "delete": true, "audit": true, "cleanup": true,
	}
	for _, e := range entries {
		if !interesting[e.Kind] {
			continue
		}
		line := e.Kind
		if e.Applied {
			line += " (applied)"
		}
		if e.Summary != "" {
			line += ": " + e.Summary
		} else if e.Prompt != "" {
			line += ": " + e.Prompt
		}
		out = append(out, line)
		if len(out) >= 12 {
			break
		}
	}
	return out
}
