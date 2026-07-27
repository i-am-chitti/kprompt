// Package recipe provides curated NL workflow packs (S-013 · T-088).
//
// Recipes expand into multi-step prompts executed through the existing route
// runner (one approval for mutating steps). They never silently mutate.
package recipe

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Recipe is one curated workflow pack.
type Recipe struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Summary  string   `json:"summary"`
	Triggers []string `json:"triggers"` // NL phrases that select this pack
	Steps    []string `json:"steps"`    // prompts; may include {{namespace}} / {{workload}}
	ReadOnly bool     `json:"readOnly"`
	Notes    []string `json:"notes,omitempty"`
}

// Catalog is the built-in OSS recipe set (distinct from Team A-030 org library).
func Catalog() []Recipe {
	out := append([]Recipe(nil), catalog...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Lookup finds a recipe by id (case-insensitive).
func Lookup(id string) (Recipe, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, r := range catalog {
		if r.ID == id {
			return r, true
		}
	}
	return Recipe{}, false
}

// Match selects a recipe from a natural-language prompt.
func Match(prompt string) (Recipe, bool) {
	p := strings.ToLower(strings.TrimSpace(prompt))
	if p == "" {
		return Recipe{}, false
	}
	type hit struct {
		r   Recipe
		len int
	}
	var best *hit
	for _, r := range catalog {
		for _, t := range r.Triggers {
			t = strings.ToLower(strings.TrimSpace(t))
			if t == "" {
				continue
			}
			if p == t || strings.Contains(p, t) {
				if best == nil || len(t) > best.len {
					best = &hit{r: r, len: len(t)}
				}
			}
		}
	}
	if best == nil {
		return Recipe{}, false
	}
	return best.r, true
}

// NeedsWorkload reports whether any step uses {{workload}}.
func (r Recipe) NeedsWorkload() bool {
	for _, s := range r.Steps {
		if strings.Contains(s, "{{workload}}") {
			return true
		}
	}
	return false
}

var workloadFromPrompt = regexp.MustCompile(`(?i)\b(?:for|workload|deployment|pod)\s+([a-z0-9][a-z0-9-]*)`)

// ExtractWorkload pulls a workload name from "… for api" / "workload api".
func ExtractWorkload(prompt string) string {
	m := workloadFromPrompt.FindStringSubmatch(prompt)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// Expand fills placeholders into step prompts.
func (r Recipe) Expand(namespace, workload string) ([]string, error) {
	ns := strings.TrimSpace(namespace)
	if ns == "" {
		ns = "default"
	}
	wl := strings.TrimSpace(workload)
	if r.NeedsWorkload() && wl == "" {
		return nil, fmt.Errorf("recipe %q requires a workload (pass --workload or say \"for <name>\")", r.ID)
	}
	out := make([]string, 0, len(r.Steps))
	for _, step := range r.Steps {
		s := strings.ReplaceAll(step, "{{namespace}}", ns)
		s = strings.ReplaceAll(s, "{{ns}}", ns)
		s = strings.ReplaceAll(s, "{{workload}}", wl)
		out = append(out, strings.TrimSpace(s))
	}
	return out, nil
}

// TryRoute matches a prompt and expands to route steps.
// ok=false means no recipe matched. err is set when a recipe matched but cannot run.
func TryRoute(prompt, namespace, workload string) (steps []string, r Recipe, ok bool, err error) {
	r, ok = Match(prompt)
	if !ok {
		return nil, Recipe{}, false, nil
	}
	wl := strings.TrimSpace(workload)
	if wl == "" {
		wl = ExtractWorkload(prompt)
	}
	steps, err = r.Expand(namespace, wl)
	if err != nil {
		return nil, r, true, err
	}
	return steps, r, true, nil
}

// FormatList is a human table for `kprompt recipe list`.
func FormatList() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-24s %-36s %s\n", "ID", "TITLE", "STEPS")
	for _, r := range Catalog() {
		ro := ""
		if r.ReadOnly {
			ro = " (read-only)"
		}
		fmt.Fprintf(&b, "%-24s %-36s %d%s\n", r.ID, r.Title, len(r.Steps), ro)
	}
	b.WriteString("\nRun:  kprompt recipe run <id> [-n namespace] [--workload name]\n")
	b.WriteString("Or:   kprompt \"harden production\" / \"prepare for black friday\"\n")
	return b.String()
}

// FormatShow prints one recipe for humans.
func FormatShow(r Recipe) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Recipe: %s — %s\n", r.ID, r.Title)
	fmt.Fprintf(&b, "%s\n\n", r.Summary)
	b.WriteString("Triggers:\n")
	for _, t := range r.Triggers {
		fmt.Fprintf(&b, "  - %s\n", t)
	}
	b.WriteString("\nSteps:\n")
	for i, s := range r.Steps {
		fmt.Fprintf(&b, "  %d. %s\n", i+1, s)
	}
	if len(r.Notes) > 0 {
		b.WriteString("\nNotes:\n")
		for _, n := range r.Notes {
			fmt.Fprintf(&b, "  - %s\n", n)
		}
	}
	b.WriteString("\nNever mutates silently — mutating steps use the same approve gate as a manual route.\n")
	return b.String()
}

var catalog = []Recipe{
	{
		ID:       "harden-production",
		Title:    "Harden production",
		Summary:  "Security hygiene audit, then idle/rightsizing review, then unused-resource cleanup — suggest patches stay approve-gated.",
		Triggers: []string{"harden production", "harden the cluster", "harden my cluster", "production harden", "security harden"},
		Steps: []string{
			"audit {{namespace}} namespace",
			"optimize {{namespace}} namespace",
			"cleanup {{namespace}} namespace",
		},
		ReadOnly: false,
		Notes: []string{
			"Audit may offer privilege-removal harden patches (approve required).",
			"Cleanup deletes (Jobs/ReplicaSets) also require approval; ConfigMap/Secret orphans stay guidance unless confirmed.",
			"No silent mutation — same PlanResult approval loop as a manual chain.",
		},
	},
	{
		ID:       "migrate-ingress-gateway",
		Title:    "Migrate Ingress → Gateway API (discover)",
		Summary:  "Read-only discovery: list Ingress, learn whether Gateway API is present, show service graph. Does not auto-rewrite Ingress objects.",
		Triggers: []string{
			"migrate ingress to gateway",
			"migrate ingress to gateway api",
			"ingress to gateway api",
			"gateway api migration",
		},
		Steps: []string{
			"list Ingress in {{namespace}}",
			"learn cluster tools",
			"show service dependency graph in {{namespace}}",
		},
		ReadOnly: true,
		Notes: []string{
			"Auto-converting Ingress to Gateway/HTTPRoute is not shipped — review graph + learn profile, then edit Git (optionally --gitops).",
			"If Gateway API is missing, install CRDs + a controller before migrating.",
		},
	},
	{
		ID:       "prepare-black-friday",
		Title:    "Prepare for Black Friday",
		Summary:  "Capacity/hygiene/drift sweep before peak traffic: optimize, audit, drift, cleanup.",
		Triggers: []string{
			"prepare for black friday",
			"black friday prep",
			"prepare for peak traffic",
			"peak traffic prep",
			"black friday ready",
		},
		Steps: []string{
			"optimize my cluster",
			"audit my cluster",
			"check cluster drift",
			"cleanup unused resources",
		},
		ReadOnly: false,
		Notes: []string{
			"Optimize/audit/drift are read-only reports; cleanup/harden suggestions still need approval.",
			"Scale-up decisions are not invented — review optimize findings before changing replicas.",
		},
	},
	{
		ID:       "crashloop-rca",
		Title:    "CrashLoop RCA chain",
		Summary:  "why → investigate → recent logs for a named workload.",
		Triggers: []string{"crashloop recipe", "crash loop recipe", "crashloop rca"},
		Steps: []string{
			"why is {{workload}} crashing in {{namespace}}",
			"investigate {{workload}} in {{namespace}}",
			"logs {{workload}} in {{namespace}}",
		},
		ReadOnly: true,
		Notes: []string{
			"kprompt recipe run crashloop-rca --workload api -n payments",
			"Optional patch suggestions from why/investigate still require approval.",
		},
	},
	{
		ID:       "oom-rca",
		Title:    "OOM RCA chain",
		Summary:  "why OOM → investigate → logs; memory bump suggestions stay approve-gated.",
		Triggers: []string{"oom recipe", "oom rca", "out of memory recipe"},
		Steps: []string{
			"why is {{workload}} OOMKilled in {{namespace}}",
			"investigate {{workload}} in {{namespace}}",
			"logs {{workload}} in {{namespace}}",
		},
		ReadOnly: true,
		Notes: []string{
			"kprompt recipe run oom-rca --workload api -n payments",
		},
	},
	{
		ID:       "imagepull-rca",
		Title:    "ImagePull RCA chain",
		Summary:  "why ImagePull → investigate → describe; auth/tag fixes stay guidance or approve-gated.",
		Triggers: []string{"imagepull recipe", "image pull recipe", "imagepull rca", "errimagepull recipe"},
		Steps: []string{
			"why is {{workload}} ImagePullBackOff in {{namespace}}",
			"investigate {{workload}} in {{namespace}}",
			"describe {{workload}} in {{namespace}}",
		},
		ReadOnly: true,
		Notes: []string{
			"kprompt recipe run imagepull-rca --workload api -n payments",
		},
	},
}
