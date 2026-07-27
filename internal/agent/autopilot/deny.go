package autopilot

import (
	"fmt"
	"regexp"
	"strings"
)

// Hard-denied action IDs — never allowlistable (AG-044).
var hardDenyActions = []string{
	"deleteNamespace",
	"wipeCluster",
	"deleteCluster",
	"deleteAllPods",
	"deleteSecret",
	"readSecretValues",
	"fabricateEvidence",
	"patchCRD",
	"deleteNode",
}

var hardDenyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^delete.*namespace`),
	regexp.MustCompile(`(?i)^wipe`),
	regexp.MustCompile(`(?i)^destroy`),
	regexp.MustCompile(`(?i)secret.*value`),
	regexp.MustCompile(`(?i)fabricat`),
	regexp.MustCompile(`(?i)clusterrole`),
	regexp.MustCompile(`(?i)^delete.*(all|every)`),
}

// HardDenyAction reports whether an action ID is globally forbidden.
func HardDenyAction(actionID string) (denied bool, reason string) {
	id := strings.TrimSpace(actionID)
	if id == "" {
		return true, "hard-deny: empty action id"
	}
	for _, d := range hardDenyActions {
		if strings.EqualFold(d, id) {
			return true, fmt.Sprintf("hard-deny: %s is never allowlistable (AG-044)", d)
		}
	}
	for _, re := range hardDenyPatterns {
		if re.MatchString(id) {
			return true, "hard-deny: action matches destructive / Secret / fabricate pattern (AG-044)"
		}
	}
	return false, ""
}

// HardDenyPlanText checks proposal plan text for wipe / Secret-value / fabricated-evidence language.
func HardDenyPlanText(summary string, steps []string) (denied bool, reason string) {
	blob := strings.ToLower(summary + " " + strings.Join(steps, " "))
	checks := []struct {
		sub string
		why string
	}{
		{"wipe the cluster", "hard-deny: wipe cluster language in plan"},
		{"delete namespace", "hard-deny: delete namespace language in plan"},
		{"secret value", "hard-deny: Secret values in plan"},
		{"fabricate evidence", "hard-deny: fabricated evidence"},
		{"invent metric", "hard-deny: fabricated evidence"},
	}
	for _, c := range checks {
		if strings.Contains(blob, c.sub) {
			return true, c.why
		}
	}
	return false, ""
}
