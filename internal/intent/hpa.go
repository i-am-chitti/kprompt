package intent

import (
	"regexp"
	"strings"

	"github.com/kprompt/kprompt/internal/tools/hpa"
)

var hpaPromptPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bhpa\b`),
	regexp.MustCompile(`(?i)\bhorizontal\s*pod\s*autoscaler\b`),
	regexp.MustCompile(`(?i)\bhorizontalpodautoscaler\b`),
	regexp.MustCompile(`(?i)\b(add|create|enable|configure)\b.{0,48}\bautoscaler\b`),
	regexp.MustCompile(`(?i)\bautoscal(e|ing)\b.{0,40}\b(cpu|memory)\b`),
}

// LooksLikeHPAPrompt reports native HorizontalPodAutoscaler create/configure language.
// KEDA / event-driven prompts stay on the keda path.
func LooksLikeHPAPrompt(prompt string) bool {
	if LooksLikeKEDAPrompt(prompt) {
		return false
	}
	for _, re := range hpaPromptPatterns {
		if re.MatchString(prompt) {
			return true
		}
	}
	return false
}

// NormalizeHPA maps HPA-shaped prompts onto kind=hpa (not KindScale / KindKEDA).
func NormalizeHPA(in Intent, prompt string) Intent {
	if !LooksLikeHPAPrompt(prompt) {
		return in
	}
	switch in.Kind {
	case KindScale, KindDeploy, KindUnknown, KindGet, KindOptimize, KindHPA:
		in.Kind = KindHPA
	default:
		if in.Kind != KindHPA {
			return in
		}
	}
	if in.Params == nil {
		in.Params = map[string]any{}
	}
	if strings.TrimSpace(in.Target.Kind) == "" ||
		strings.EqualFold(in.Target.Kind, "Deployment") ||
		strings.EqualFold(in.Target.Kind, "HPA") {
		in.Target.Kind = "HorizontalPodAutoscaler"
	}
	target := strings.TrimSpace(in.Target.Name)
	if t, ok := in.StringParam("target"); ok {
		target = t
	}
	if target != "" {
		if _, ok := in.StringParam("target"); !ok {
			in.Params["target"] = target
		}
	}
	if _, ok := in.StringParam("hpaName"); !ok && target != "" {
		in.Params["hpaName"] = hpa.DefaultHPAName(target)
	}
	return in
}
