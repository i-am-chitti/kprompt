package intent

import (
	"regexp"
	"strings"
)

var (
	architecturePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bexplain\s+(?:the\s+)?architecture\b`),
		regexp.MustCompile(`(?i)\barchitecture\s+(?:of|for|overview)\b`),
		regexp.MustCompile(`(?i)\b(?:cluster|platform|namespace)\s+architecture\b`),
		regexp.MustCompile(`(?i)\bwhat\s+does\s+(?:this\s+)?(?:cluster|namespace|platform)\s+look\s+like\b`),
		regexp.MustCompile(`(?i)\bplatform\s+overview\b`),
		regexp.MustCompile(`(?i)\bdescribe\s+(?:the\s+)?(?:cluster|platform)\s+architecture\b`),
	}
	clusterArchitecturePattern = regexp.MustCompile(`(?i)\b(?:architecture|overview)\b.*\bcluster\b|\bcluster\b.*\b(?:architecture|overview|look\s+like)\b|\bwhat\s+does\s+(?:this\s+)?cluster\b`)
)

// LooksLikeArchitecturePrompt detects high-level architecture narrative asks (S-012).
func LooksLikeArchitecturePrompt(prompt string) bool {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return false
	}
	if LooksLikeGraphPrompt(prompt) {
		return false
	}
	for _, re := range architecturePatterns {
		if re.MatchString(prompt) {
			return true
		}
	}
	return false
}

// NormalizeArchitecture maps architecture prompts onto KindArchitecture.
func NormalizeArchitecture(in Intent, prompt string) Intent {
	if !LooksLikeArchitecturePrompt(prompt) {
		return in
	}
	switch in.Kind {
	case KindGet, KindExplain, KindDescribe, KindUnknown, KindGraph, KindLearn, KindArchitecture:
		in.Kind = KindArchitecture
	default:
		return in
	}
	if strings.TrimSpace(in.Target.Kind) == "" {
		in.Target.Kind = "Namespace"
	}
	if in.Params == nil {
		in.Params = map[string]any{}
	}
	if clusterArchitecturePattern.MatchString(prompt) {
		in.Params["scope"] = "cluster"
	}
	in.Target.Name = ""
	return in
}

// ApplyArchitectureScope clears the default namespace for cluster-wide architecture
// prompts unless the CLI forced -n or the prompt named a namespace.
func ApplyArchitectureScope(in Intent, prompt string, prefs ScopePrefs) Intent {
	if in.Kind != KindArchitecture {
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
