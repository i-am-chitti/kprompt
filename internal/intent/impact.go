package intent

import (
	"regexp"
	"strings"
)

var (
	impactPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bimpact\b`),
		regexp.MustCompile(`(?i)\bblast\s+radius\b`),
		regexp.MustCompile(`(?i)\bwho\s+(?:uses|consumes|calls)\b`),
		regexp.MustCompile(`(?i)\bwhat\s+depends\s+on\b`),
		regexp.MustCompile(`(?i)\bdependents?\s+(?:of|for)\b`),
	}
	impactTargetPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:impact|blast\s+radius)\s+(?:of|for)\s+(?:the\s+)?(?:(service|deployment)\s+)?([a-z0-9][a-z0-9-]*)`),
		regexp.MustCompile(`(?i)\bwho\s+(?:uses|consumes|calls)\s+(?:the\s+)?(?:(service|deployment)\s+)?([a-z0-9][a-z0-9-]*)`),
		regexp.MustCompile(`(?i)\bwhat\s+depends\s+on\s+(?:the\s+)?(?:(service|deployment)\s+)?([a-z0-9][a-z0-9-]*)`),
	}
)

// LooksLikeImpactPrompt detects reverse dependency / blast-radius reads (S-005).
func LooksLikeImpactPrompt(prompt string) bool {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return false
	}
	for _, re := range impactPatterns {
		if re.MatchString(prompt) {
			return true
		}
	}
	return false
}

// NormalizeImpact maps consumer / blast asks onto KindImpact.
func NormalizeImpact(in Intent, prompt string) Intent {
	if !LooksLikeImpactPrompt(prompt) {
		return in
	}
	switch in.Kind {
	case KindExplain, KindGet, KindDescribe, KindUnknown, KindGraph, KindImpact:
		in.Kind = KindImpact
	default:
		return in
	}
	for _, re := range impactTargetPatterns {
		match := re.FindStringSubmatch(prompt)
		if len(match) != 3 {
			continue
		}
		if strings.TrimSpace(in.Target.Kind) == "" && match[1] != "" {
			in.Target.Kind = strings.ToUpper(match[1][:1]) + strings.ToLower(match[1][1:])
		}
		if strings.TrimSpace(in.Target.Name) == "" {
			in.Target.Name = strings.ToLower(match[2])
		}
		break
	}
	if strings.TrimSpace(in.Target.Kind) == "" {
		if strings.Contains(strings.ToLower(prompt), "deployment ") {
			in.Target.Kind = "Deployment"
		} else {
			in.Target.Kind = "Service"
		}
	}
	return in
}
