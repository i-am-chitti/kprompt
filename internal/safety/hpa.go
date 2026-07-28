package safety

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/kprompt/kprompt/internal/planner"
)

var hpaWipePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(delete|remove|wipe)\b.*\b(all|every)\b.*\b(hpa|horizontalpodautoscaler|autoscaler)`),
	regexp.MustCompile(`(?i)\b(hpa|horizontalpodautoscaler|autoscaler)s?\b.*\b(delete|remove|wipe)\b.*\b(all|every)\b`),
}

// CheckHPAPrompt denies wipe-class HPA prompts.
func CheckHPAPrompt(prompt string) Result {
	p := strings.TrimSpace(prompt)
	for _, re := range hpaWipePatterns {
		if re.MatchString(p) {
			return Result{
				Risk:    RiskDenied,
				Denied:  true,
				Message: "🛡️ Refusing wipe-class HPA delete — name a single HorizontalPodAutoscaler",
			}
		}
	}
	return Result{Risk: RiskLow}
}

func evaluateHPAPlan(plan planner.ExecutionPlan) Result {
	for _, a := range plan.Actions {
		if a.Object.Kind != "HorizontalPodAutoscaler" {
			continue
		}
		switch a.Op {
		case planner.OpCreate, planner.OpUpdate:
			continue
		default:
			return Result{
				Risk:    RiskDenied,
				Denied:  true,
				Message: fmt.Sprintf("🛡️ Refusing unsupported HPA action %q", a.Op),
			}
		}
	}
	return Result{Risk: RiskLow}
}
