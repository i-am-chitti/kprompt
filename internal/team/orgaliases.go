package team

import (
	"strings"

	"github.com/kprompt/kprompt/internal/config"
)

// MergeAliases builds the effective alias map for A-075.
// Org aliases win on key conflict (case-insensitive); local may add keys org does not define.
func MergeAliases(org, local map[string]string) map[string]string {
	if len(org) == 0 && len(local) == 0 {
		return nil
	}
	out := make(map[string]string)
	index := map[string]string{} // lower → canonical key in out
	put := func(k, v string, overwrite bool) {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" || v == "" {
			return
		}
		low := strings.ToLower(k)
		if canon, ok := index[low]; ok {
			if overwrite {
				delete(out, canon)
				out[k] = v
				index[low] = k
			}
			return
		}
		out[k] = v
		index[low] = k
	}
	for k, v := range org {
		put(k, v, true)
	}
	for k, v := range local {
		put(k, v, false)
	}
	return out
}

// ApplyOrgContextPolicy overlays cached org context_aliases and require_alias_match (A-075).
// Org require_alias_match can only tighten (true forces on); never clears a local true.
// Re-resolves cfg.Context using the prior alias key when present.
func ApplyOrgContextPolicy(cfg *config.Resolved) {
	if cfg == nil {
		return
	}
	pol, ok, err := LoadPolicy()
	if err != nil || !ok {
		return
	}
	if len(pol.ContextAliases) == 0 && !pol.RequireAliasMatch {
		return
	}
	lookup := cfg.ContextAlias
	if lookup == "" {
		lookup = cfg.Context
	}
	cfg.Aliases = MergeAliases(pol.ContextAliases, cfg.Aliases)
	if pol.RequireAliasMatch {
		cfg.RequireAliasMatch = true
	}
	if lookup != "" {
		resolved, alias := config.ResolveContext(lookup, cfg.Aliases)
		cfg.Context = resolved
		cfg.ContextAlias = alias
	}
}
