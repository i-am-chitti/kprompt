package intent

import (
	"regexp"
	"strings"
)

var (
	driftPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bdrift\b`),
		regexp.MustCompile(`(?i)\bout[- ]?of[- ]?sync\b`),
		regexp.MustCompile(`(?i)\b(cluster|gitops)\s+drift\b`),
		regexp.MustCompile(`(?i)\b(check|detect|find|show)\s+drift\b`),
		regexp.MustCompile(`(?i)\bdrift\s+(vs|versus|against)\s+git\b`),
	}
	clusterDriftPattern = regexp.MustCompile(`(?i)\b(?:drift|out[- ]?of[- ]?sync)\b.*\bcluster\b|\bcluster\b.*\b(?:drift|out[- ]?of[- ]?sync)\b`)
)

// LooksLikeDriftPrompt detects GitOps drift / out-of-sync asks (S-008).
func LooksLikeDriftPrompt(prompt string) bool {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return false
	}
	// Plain "gitops status/sync" stays KindGitOps — require drift/out-of-sync language.
	for _, re := range driftPatterns {
		if re.MatchString(prompt) {
			return true
		}
	}
	return false
}

// NormalizeDrift maps drift phrasing onto KindDrift (wins over status-shaped gitops).
func NormalizeDrift(in Intent, prompt string) Intent {
	if !LooksLikeDriftPrompt(prompt) {
		return in
	}
	switch in.Kind {
	case KindGet, KindExplain, KindDescribe, KindUnknown, KindGitOps, KindLearn, KindDrift:
		in.Kind = KindDrift
	default:
		return in
	}
	if strings.TrimSpace(in.Target.Kind) == "" {
		in.Target.Kind = "Cluster"
	}
	if in.Params == nil {
		in.Params = map[string]any{}
	}
	if clusterDriftPattern.MatchString(prompt) {
		in.Params["scope"] = "cluster"
	}
	p := strings.ToLower(prompt)
	if strings.Contains(p, "flux") {
		in.Params["engine"] = "flux"
	} else if strings.Contains(p, "argocd") || strings.Contains(p, "argo cd") {
		in.Params["engine"] = "argocd"
	}
	return in
}

// ApplyDriftScope clears the default namespace for cluster-wide drift prompts
// unless the CLI forced -n or the prompt named a namespace.
func ApplyDriftScope(in Intent, prompt string, prefs ScopePrefs) Intent {
	if in.Kind != KindDrift {
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
