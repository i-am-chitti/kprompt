package intent

import (
	"regexp"
	"strings"
)

var (
	auditPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\baudit\b`),
		regexp.MustCompile(`(?i)\bsecurity\s+scan\b`),
		regexp.MustCompile(`(?i)\bhygiene\s+(?:scan|check)\b`),
		regexp.MustCompile(`(?i)\bharden\b.*\b(?:namespace|cluster|workloads?)\b`),
		regexp.MustCompile(`(?i)\b(?:namespace|cluster|workloads?)\b.*\bharden\b`),
	}
	clusterAuditPattern = regexp.MustCompile(`(?i)\b(?:audit|security\s+scan|hygiene)\b.*\bcluster\b|\bcluster\b.*\b(?:audit|security\s+scan|hygiene)\b`)
)

// LooksLikeAuditPrompt detects security / hygiene scan asks (S-006).
func LooksLikeAuditPrompt(prompt string) bool {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return false
	}
	for _, re := range auditPatterns {
		if re.MatchString(prompt) {
			return true
		}
	}
	return false
}

// NormalizeAudit maps hygiene / security scan prompts onto KindAudit.
func NormalizeAudit(in Intent, prompt string) Intent {
	if !LooksLikeAuditPrompt(prompt) {
		return in
	}
	switch in.Kind {
	case KindGet, KindExplain, KindDescribe, KindUnknown, KindOptimize, KindAudit:
		in.Kind = KindAudit
	default:
		return in
	}
	if strings.TrimSpace(in.Target.Kind) == "" {
		in.Target.Kind = "Namespace"
	}
	if in.Params == nil {
		in.Params = map[string]any{}
	}
	if clusterAuditPattern.MatchString(prompt) {
		in.Params["scope"] = "cluster"
	}
	return in
}

// ApplyAuditScope clears the default namespace for cluster-wide audit prompts
// unless the CLI forced -n or the prompt named a namespace.
func ApplyAuditScope(in Intent, prompt string, prefs ScopePrefs) Intent {
	if in.Kind != KindAudit {
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
