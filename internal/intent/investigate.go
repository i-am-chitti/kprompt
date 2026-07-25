package intent

import (
	"regexp"
	"strings"
)

var reInvestigateTarget = regexp.MustCompile(
	`(?i)\binvestigate\s+(?:the\s+)?(?:deployment\s+|pod\s+)?([a-z0-9][a-z0-9-]*)`,
)

// LooksLikeInvestigatePrompt detects multi-hop RCA / investigate phrasing.
func LooksLikeInvestigatePrompt(prompt string) bool {
	p := strings.ToLower(strings.TrimSpace(prompt))
	if p == "" {
		return false
	}
	if strings.Contains(p, "investigate ") || strings.HasPrefix(p, "investigate") {
		return true
	}
	if strings.Contains(p, "root cause") || strings.Contains(p, "rca ") || strings.HasSuffix(p, " rca") {
		return true
	}
	if strings.Contains(p, "multi-hop") || strings.Contains(p, "deep dive") {
		return true
	}
	return false
}

// NormalizeInvestigate maps investigate-shaped prompts onto KindInvestigate.
// Crash-focused "why is X crashing" is KindWhy (S-003); investigate is the
// multi-hop Service→…→Logs path (S-002).
func NormalizeInvestigate(in Intent, prompt string) Intent {
	if !LooksLikeInvestigatePrompt(prompt) {
		return in
	}
	switch in.Kind {
	case KindExplain, KindGet, KindDescribe, KindUnknown, KindInvestigate:
		in.Kind = KindInvestigate
	default:
		return in
	}
	if strings.TrimSpace(in.Target.Kind) == "" {
		in.Target.Kind = "Deployment"
	}
	if strings.TrimSpace(in.Target.Name) == "" {
		if m := reInvestigateTarget.FindStringSubmatch(prompt); len(m) == 2 {
			in.Target.Name = m[1]
		}
	}
	return in
}
