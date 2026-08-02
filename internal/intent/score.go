package intent

import (
	"regexp"
	"strings"
)

var (
	scorePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bscorecard\b`),
		regexp.MustCompile(`(?i)\bscore\b.*\b(?:cluster|namespace|security|reliability|cost)\b`),
		regexp.MustCompile(`(?i)\b(?:cluster|namespace)\b.*\bscore\b`),
		regexp.MustCompile(`(?i)\bhealth\s+score\b`),
		regexp.MustCompile(`(?i)\bscore\s+(?:my\s+)?(?:cluster|namespace)\b`),
	}
	clusterScorePattern = regexp.MustCompile(`(?i)\b(?:score|scorecard)\b.*\bcluster\b|\bcluster\b.*\b(?:score|scorecard)\b`)
)

// LooksLikeScorePrompt detects scorecard asks (S-011).
func LooksLikeScorePrompt(prompt string) bool {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" || LooksLikeRoastPrompt(prompt) {
		return false
	}
	for _, re := range scorePatterns {
		if re.MatchString(prompt) {
			return true
		}
	}
	return false
}

// NormalizeScore maps scorecard prompts onto KindScore.
func NormalizeScore(in Intent, prompt string) Intent {
	if !LooksLikeScorePrompt(prompt) {
		return in
	}
	switch in.Kind {
	case KindGet, KindExplain, KindDescribe, KindUnknown, KindOptimize, KindAudit, KindRoast, KindScore:
		in.Kind = KindScore
	default:
		return in
	}
	if strings.TrimSpace(in.Target.Kind) == "" {
		in.Target.Kind = "Namespace"
	}
	if in.Params == nil {
		in.Params = map[string]any{}
	}
	if clusterScorePattern.MatchString(prompt) {
		in.Params["scope"] = "cluster"
	}
	in.Target.Name = ""
	return in
}

// ApplyScoreScope clears the default namespace for cluster-wide score prompts
// unless the CLI forced -n or the prompt named a namespace.
func ApplyScoreScope(in Intent, prompt string, prefs ScopePrefs) Intent {
	if in.Kind != KindScore {
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
