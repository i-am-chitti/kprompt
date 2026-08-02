package intent

import (
	"regexp"
	"strings"
)

var (
	searchPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bsearch\b`),
		regexp.MustCompile(`(?i)\bfind\s+every\b`),
		regexp.MustCompile(`(?i)\bfind\s+all\b`),
		regexp.MustCompile(`(?i)\bwhich\s+(?:deployments?|pods?|services?|statefulsets?|daemonsets?|workloads?)\b`),
		regexp.MustCompile(`(?i)\binventory\s+(?:query|search)\b`),
		regexp.MustCompile(`(?i)\b(?:deployments?|pods?|services?|workloads?)\b.+\b(?:using|with)\b`),
	}
	clusterSearchPattern = regexp.MustCompile(`(?i)\b(?:search|find\s+(?:every|all)|which)\b.*\bcluster\b|\bcluster\b.*\b(?:search|find)\b`)
	searchTermUsing      = regexp.MustCompile(`(?i)\b(?:using|with|for)\s+([a-z0-9][a-z0-9._:/-]*)`)
	searchTermFor        = regexp.MustCompile(`(?i)\bsearch\s+(?:for\s+)?([a-z0-9][a-z0-9._:/-]*)`)
	searchKindPattern    = regexp.MustCompile(`(?i)\b(deployments?|statefulsets?|daemonsets?|pods?|services?|workloads?)\b`)
	searchMatchPattern   = regexp.MustCompile(`(?i)\b(?:by\s+)?(image|env|label|name|annotation|command)s?\b`)
)

// LooksLikeSearchPrompt detects structured inventory search asks (S-010).
func LooksLikeSearchPrompt(prompt string) bool {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return false
	}
	if LooksLikeCleanupPrompt(prompt) {
		return false
	}
	for _, re := range searchPatterns {
		if re.MatchString(prompt) {
			return true
		}
	}
	return false
}

// NormalizeSearch maps inventory search prompts onto KindSearch.
func NormalizeSearch(in Intent, prompt string) Intent {
	if !LooksLikeSearchPrompt(prompt) {
		return in
	}
	switch in.Kind {
	case KindGet, KindExplain, KindDescribe, KindUnknown, KindImpact, KindSearch:
		in.Kind = KindSearch
	default:
		return in
	}
	if in.Params == nil {
		in.Params = map[string]any{}
	}
	if _, ok := in.StringParam("query"); !ok {
		if term := InferSearchQuery(prompt); term != "" {
			in.Params["query"] = term
		}
	}
	if strings.TrimSpace(in.Target.Kind) == "" {
		if k := InferSearchKind(prompt); k != "" {
			in.Target.Kind = k
		} else {
			in.Target.Kind = "Deployment"
		}
	}
	if _, ok := in.StringParam("match"); !ok {
		if m := InferSearchMatch(prompt); m != "" {
			in.Params["match"] = m
		}
	}
	if clusterSearchPattern.MatchString(prompt) {
		in.Params["scope"] = "cluster"
	}
	// Search is not a named-object get — drop accidental target.name.
	in.Target.Name = ""
	return in
}

// ApplySearchScope clears the default namespace for cluster-wide search prompts
// unless the CLI forced -n or the prompt named a namespace.
func ApplySearchScope(in Intent, prompt string, prefs ScopePrefs) Intent {
	if in.Kind != KindSearch {
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

// InferSearchQuery extracts a likely match term from NL.
func InferSearchQuery(prompt string) string {
	p := strings.TrimSpace(prompt)
	if m := searchTermUsing.FindStringSubmatch(p); len(m) == 2 {
		return stripSearchStop(m[1])
	}
	if m := searchTermFor.FindStringSubmatch(p); len(m) == 2 {
		return stripSearchStop(m[1])
	}
	return ""
}

// InferSearchKind extracts a resource kind filter from NL.
func InferSearchKind(prompt string) string {
	m := searchKindPattern.FindStringSubmatch(prompt)
	if len(m) < 2 {
		return ""
	}
	switch strings.ToLower(m[1]) {
	case "deployment", "deployments":
		return "Deployment"
	case "statefulset", "statefulsets":
		return "StatefulSet"
	case "daemonset", "daemonsets":
		return "DaemonSet"
	case "pod", "pods":
		return "Pod"
	case "service", "services":
		return "Service"
	case "workload", "workloads":
		return ""
	default:
		return ""
	}
}

// InferSearchMatch extracts an optional field filter from NL.
func InferSearchMatch(prompt string) string {
	m := searchMatchPattern.FindStringSubmatch(prompt)
	if len(m) < 2 {
		return ""
	}
	return strings.ToLower(m[1])
}

func stripSearchStop(s string) string {
	s = strings.Trim(s, ".,;:!?\"'")
	switch strings.ToLower(s) {
	case "the", "a", "an", "my", "our", "every", "all", "cluster", "namespace":
		return ""
	default:
		return s
	}
}
