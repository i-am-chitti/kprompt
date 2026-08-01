package intent

import (
	"regexp"
	"strings"
)

var (
	roastPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bhow'?s\s+(?:my\s+)?(?:cluster|namespace)\b`),
		regexp.MustCompile(`(?i)\bhow\s+is\s+(?:my\s+)?(?:cluster|namespace)\b`),
		regexp.MustCompile(`(?i)\bhow'?s\s+[a-z0-9][a-z0-9-]*\s+looking\b`),
		regexp.MustCompile(`(?i)\broast\b.*\b(?:my\s+)?(?:cluster|namespace)\b`),
		regexp.MustCompile(`(?i)\b(?:cluster|namespace)\b.*\broast\b`),
		regexp.MustCompile(`(?i)\bcluster\s+roast\b`),
		regexp.MustCompile(`(?i)\brate\s+my\s+(?:cluster|namespace)\b`),
		regexp.MustCompile(`(?i)\bvibe\s*check\b.*\b(?:cluster|namespace)\b`),
		regexp.MustCompile(`(?i)\b(?:cluster|namespace)\b.*\bvibe\s*check\b`),
	}
	clusterRoastPattern = regexp.MustCompile(`(?i)\b(?:how'?s|how\s+is|roast|rate|vibe\s*check)\b.*\bcluster\b|\bcluster\b.*\b(?:roast|vibe\s*check|health)\b`)
)

// LooksLikeRoastPrompt detects playful cluster/namespace health roast asks.
func LooksLikeRoastPrompt(prompt string) bool {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return false
	}
	for _, re := range roastPatterns {
		if re.MatchString(prompt) {
			return true
		}
	}
	return false
}

// NormalizeRoast maps roast / vibe-check prompts onto kind=roast.
func NormalizeRoast(in Intent, prompt string) Intent {
	if !LooksLikeRoastPrompt(prompt) {
		return in
	}
	switch in.Kind {
	case KindGet, KindExplain, KindDescribe, KindUnknown, KindOptimize, KindRoast:
		in.Kind = KindRoast
	default:
		return in
	}
	if strings.TrimSpace(in.Target.Kind) == "" {
		in.Target.Kind = "Namespace"
	}
	if in.Params == nil {
		in.Params = map[string]any{}
	}
	if clusterRoastPattern.MatchString(prompt) {
		in.Params["scope"] = "cluster"
	}
	return in
}

// ApplyRoastScope clears the default namespace for cluster-wide roast prompts
// unless the CLI forced -n or the prompt named a namespace.
func ApplyRoastScope(in Intent, prompt string, prefs ScopePrefs) Intent {
	if in.Kind != KindRoast {
		return in
	}
	if prefs.ForceNamespace {
		if in.Params != nil {
			delete(in.Params, "scope")
		}
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
