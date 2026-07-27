// Package learn builds and persists a per-context cluster tool profile (S-009 · T-087).
//
// Detection reuses tools.Detect. Profiles are local-only (~/.kprompt/profiles)
// and never mutate the cluster. PromptBias may bias intent routing toward the
// detected stack when a profile exists.
package learn

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kprompt/kprompt/internal/config"
	"github.com/kprompt/kprompt/internal/tools"
)

const Version = 1

// ToolEntry is one tool row in a learned profile.
type ToolEntry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Detail    string `json:"detail,omitempty"`
	Available bool   `json:"available"`
}

// Profile is the persisted cluster tool profile for one kube context.
type Profile struct {
	Version   int         `json:"version"`
	Context   string      `json:"context"`
	LearnedAt time.Time   `json:"learned_at"`
	Tools     []ToolEntry `json:"tools"`
	Available []string    `json:"available"` // stable ids for quick bias
}

// Options configures Run.
type Options struct {
	Context string
	File    config.File
	// Detect overrides tools.Detect (tests).
	Detect func(ctx context.Context, opts tools.DetectOptions) (*tools.Registry, error)
	// Store overrides the default file store (tests).
	Store Store
}

// Run detects integrations, persists a profile, and returns it.
func Run(ctx context.Context, opts Options) (Profile, error) {
	detect := opts.Detect
	if detect == nil {
		detect = tools.Detect
	}
	store := opts.Store
	if store == nil {
		store = DefaultStore()
	}

	kubeCtx := strings.TrimSpace(opts.Context)
	if kubeCtx == "" {
		kubeCtx = strings.TrimSpace(opts.File.Context)
	}

	reg, err := detect(ctx, tools.DetectOptions{
		Context: kubeCtx,
		File:    opts.File,
	})
	if err != nil {
		return Profile{}, err
	}

	// Prefer the connected context name when Detect filled kubernetes detail.
	if kubeCtx == "" {
		if r, ok := reg.Get(tools.IDKubernetes); ok {
			if strings.HasPrefix(r.Detail, "context: ") {
				kubeCtx = strings.TrimPrefix(r.Detail, "context: ")
			}
		}
	}

	prof := FromRegistry(kubeCtx, reg)
	if err := store.Save(prof); err != nil {
		return Profile{}, err
	}
	return prof, nil
}

// FromRegistry builds a Profile from a tools.Registry snapshot.
func FromRegistry(kubeCtx string, reg *tools.Registry) Profile {
	prof := Profile{
		Version:   Version,
		Context:   strings.TrimSpace(kubeCtx),
		LearnedAt: time.Now().UTC(),
	}
	if reg == nil {
		return prof
	}
	for _, r := range reg.All() {
		entry := ToolEntry{
			ID:        string(r.ID),
			Name:      r.Name,
			Status:    string(r.Status),
			Detail:    r.Detail,
			Available: r.Available(),
		}
		prof.Tools = append(prof.Tools, entry)
		if entry.Available && r.ID != tools.IDKubernetes {
			prof.Available = append(prof.Available, entry.ID)
		}
	}
	return prof
}

// AvailableNames returns human names for available non-kubernetes tools.
func (p Profile) AvailableNames() []string {
	out := make([]string, 0, len(p.Available))
	byID := make(map[string]string, len(p.Tools))
	for _, t := range p.Tools {
		byID[t.ID] = t.Name
	}
	for _, id := range p.Available {
		if name := byID[id]; name != "" {
			out = append(out, name)
		} else {
			out = append(out, id)
		}
	}
	return out
}

// Summary is a one-line doctor / CLI summary.
func (p Profile) Summary() string {
	names := p.AvailableNames()
	ctx := p.Context
	if ctx == "" {
		ctx = "(default)"
	}
	when := p.LearnedAt.UTC().Format(time.RFC3339)
	if len(names) == 0 {
		return fmt.Sprintf("context %s · learned %s · no optional tools detected", ctx, when)
	}
	return fmt.Sprintf("context %s · learned %s · available: %s", ctx, when, strings.Join(names, ", "))
}

// PromptBias is a short system-prompt addon so intent extraction prefers the
// detected stack (e.g. Gateway API over Ingress when present). Empty when
// nothing useful was learned.
func (p Profile) PromptBias() string {
	names := p.AvailableNames()
	if len(names) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Cluster tool profile (from kprompt learn; prefer these when relevant, do not invent APIs):\n")
	b.WriteString("- Available: ")
	b.WriteString(strings.Join(names, ", "))
	b.WriteByte('\n')
	for _, t := range p.Tools {
		if !t.Available || t.ID == string(tools.IDKubernetes) {
			continue
		}
		detail := strings.TrimSpace(t.Detail)
		if detail == "" {
			continue
		}
		fmt.Fprintf(&b, "- %s: %s\n", t.Name, detail)
	}
	b.WriteString("Routing hints: if Gateway API is available prefer it over Ingress; if Linkerd and Istio both absent stay on Service/Ingress; if GitOps (Flux/Argo CD) is available prefer gitops for sync/health; if Helm is available prefer install/upgrade for charts; if Prometheus is available prefer performance for latency asks.\n")
	return b.String()
}

// Encode marshals a profile for tests / JSON CLI.
func Encode(p Profile) ([]byte, error) {
	return json.MarshalIndent(p, "", "  ")
}

// Decode unmarshals a profile.
func Decode(raw []byte) (Profile, error) {
	var p Profile
	if err := json.Unmarshal(raw, &p); err != nil {
		return Profile{}, err
	}
	if p.Version == 0 {
		p.Version = Version
	}
	return p, nil
}
