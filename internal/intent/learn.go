package intent

import (
	"regexp"
	"strings"
)

var learnPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\blearn\b.*\b(cluster|tools?|profile|stack|integrations?)\b`),
	regexp.MustCompile(`(?i)\b(cluster|tools?|profile|stack|integrations?)\b.*\blearn\b`),
	regexp.MustCompile(`(?i)^\s*learn\s*$`),
	regexp.MustCompile(`(?i)\blearn\s+(my\s+)?(cluster|tools?|profile)\b`),
	regexp.MustCompile(`(?i)\bdetect\s+(cluster\s+)?(tools?|integrations?|stack)\b`),
	regexp.MustCompile(`(?i)\btool\s+profile\b`),
}

// LooksLikeLearnPrompt detects cluster tool profile / learn phrasing (S-009).
func LooksLikeLearnPrompt(prompt string) bool {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return false
	}
	for _, re := range learnPatterns {
		if re.MatchString(prompt) {
			return true
		}
	}
	return false
}

// NormalizeLearn maps learn / tool-profile prompts onto KindLearn.
func NormalizeLearn(in Intent, prompt string) Intent {
	if !LooksLikeLearnPrompt(prompt) {
		return in
	}
	switch in.Kind {
	case KindGet, KindExplain, KindDescribe, KindUnknown, KindLearn, KindGitOps, KindIstio:
		in.Kind = KindLearn
	default:
		return in
	}
	if strings.TrimSpace(in.Target.Kind) == "" {
		in.Target.Kind = "Cluster"
	}
	return in
}
