package intent

import (
	"regexp"
	"strings"
)

var (
	cleanupPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bcleanup\b`),
		regexp.MustCompile(`(?i)\bclean\s+up\b`),
		regexp.MustCompile(`(?i)\b(unused|orphan(ed)?|stale|leftover|dangling)\b.*\b(resources?|configmaps?|secrets?|jobs?|replicasets?)\b`),
		regexp.MustCompile(`(?i)\bprune\b.*\b(resources?|configmaps?|secrets?|jobs?|replicasets?|cluster|namespace)\b`),
	}
	clusterCleanupPattern = regexp.MustCompile(`(?i)\b(?:cleanup|clean\s+up|prune)\b.*\bcluster\b|\bcluster\b.*\b(?:cleanup|clean\s+up|prune)\b`)
)

// LooksLikeCleanupPrompt detects unused / stale resource cleanup asks (S-007).
func LooksLikeCleanupPrompt(prompt string) bool {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return false
	}
	for _, re := range cleanupPatterns {
		if re.MatchString(prompt) {
			return true
		}
	}
	return false
}

// NormalizeCleanup maps prune / unused resource prompts onto KindCleanup.
func NormalizeCleanup(in Intent, prompt string) Intent {
	if !LooksLikeCleanupPrompt(prompt) {
		return in
	}
	switch in.Kind {
	case KindGet, KindExplain, KindDescribe, KindUnknown, KindDelete, KindCleanup:
		in.Kind = KindCleanup
	default:
		return in
	}
	if strings.TrimSpace(in.Target.Kind) == "" {
		in.Target.Kind = "Namespace"
	}
	if in.Params == nil {
		in.Params = map[string]any{}
	}
	if clusterCleanupPattern.MatchString(prompt) {
		in.Params["scope"] = "cluster"
	}
	return in
}

// ApplyCleanupScope clears the default namespace for cluster-wide cleanup prompts
// unless the CLI forced -n or the prompt named a namespace.
func ApplyCleanupScope(in Intent, prompt string, prefs ScopePrefs) Intent {
	if in.Kind != KindCleanup {
		return in
	}
	if prefs.ForceNamespace {
		return in
	}
	phraseNS, _ := ParseScopePhrases(prompt)
	if phraseNS != "" {
		in.Target.Namespace = phraseNS
		if in.Params != nil {
			delete(in.Params, "scope")
		}
		return in
	}
	if scope, ok := in.StringParam("scope"); ok && scope == "cluster" {
		in.Target.Namespace = ""
	}
	return in
}
