package intent

import (
	"regexp"
	"strings"
)

var (
	timelinePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\btimeline\b`),
		regexp.MustCompile(`(?i)\bchronolog`),
		regexp.MustCompile(`(?i)\bwhat\s+happened\s+(?:to|with)\b`),
		regexp.MustCompile(`(?i)\bincident\s+history\b`),
		regexp.MustCompile(`(?i)\bevent\s+history\b`),
	}
	timelineTargetPattern = regexp.MustCompile(
		`(?i)\b(?:timeline|chronology|history)\s+(?:for\s+|of\s+)?(?:my\s+|the\s+)?(?:pod\s+|deployment\s+)?([a-z0-9][a-z0-9-]*)`,
	)
	timelineWhatHappenedTarget = regexp.MustCompile(
		`(?i)\bwhat\s+happened\s+(?:to|with)\s+(?:my\s+|the\s+)?(?:pod\s+|deployment\s+)?([a-z0-9][a-z0-9-]*)`,
	)
)

// LooksLikeTimelinePrompt detects incident chronology asks (S-004).
func LooksLikeTimelinePrompt(prompt string) bool {
	p := strings.ToLower(strings.TrimSpace(prompt))
	if p == "" {
		return false
	}
	for _, re := range timelinePatterns {
		if re.MatchString(p) {
			return true
		}
	}
	return false
}

// NormalizeTimeline maps chronology prompts onto KindTimeline.
func NormalizeTimeline(in Intent, prompt string) Intent {
	if !LooksLikeTimelinePrompt(prompt) {
		return in
	}
	switch in.Kind {
	case KindExplain, KindGet, KindDescribe, KindUnknown, KindTimeline, KindInvestigate, KindWhy:
		in.Kind = KindTimeline
	default:
		return in
	}
	if strings.TrimSpace(in.Target.Kind) == "" {
		lower := strings.ToLower(prompt)
		if strings.Contains(lower, "pod ") {
			in.Target.Kind = "Pod"
		} else {
			in.Target.Kind = "Deployment"
		}
	}
	if strings.TrimSpace(in.Target.Name) == "" {
		if m := timelineTargetPattern.FindStringSubmatch(prompt); len(m) == 2 {
			in.Target.Name = strings.ToLower(m[1])
		} else if m := timelineWhatHappenedTarget.FindStringSubmatch(prompt); len(m) == 2 {
			in.Target.Name = strings.ToLower(m[1])
		}
	}
	if in.Params == nil {
		in.Params = map[string]any{}
	}
	if _, ok := in.Window(); !ok {
		in.Params["window"] = "1h"
	}
	return in
}
