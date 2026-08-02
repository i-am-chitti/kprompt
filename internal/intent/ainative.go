package intent

import (
	"regexp"
	"strings"
)

var (
	sessionPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bwhat\s+did\s+i\s+do\s+today\b`),
		regexp.MustCompile(`(?i)\bsession\s+(?:digest|summary|today)\b`),
		regexp.MustCompile(`(?i)\btoday'?s?\s+(?:session|digest|history|actions)\b`),
		regexp.MustCompile(`(?i)\bday\s+digest\b`),
	}
	rememberPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^remember\b`),
		regexp.MustCompile(`(?i)\bremember\s+that\b`),
		regexp.MustCompile(`(?i)^forget\b`),
	}
	watchPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^watch\b`),
		regexp.MustCompile(`(?i)\bwatch\s+(?:the\s+)?(?:namespace|cluster|payments|pods?)\b`),
		regexp.MustCompile(`(?i)\bproactive\s+(?:watch|scan|assist)`),
	}
)

// LooksLikeSessionPrompt detects day-digest asks (S-016).
func LooksLikeSessionPrompt(prompt string) bool {
	prompt = strings.TrimSpace(prompt)
	for _, re := range sessionPatterns {
		if re.MatchString(prompt) {
			return true
		}
	}
	return false
}

// LooksLikeRememberPrompt detects remember/forget statements (S-015).
func LooksLikeRememberPrompt(prompt string) bool {
	prompt = strings.TrimSpace(prompt)
	for _, re := range rememberPatterns {
		if re.MatchString(prompt) {
			return true
		}
	}
	return false
}

// LooksLikeWatchPrompt detects laptop watch asks (S-014).
func LooksLikeWatchPrompt(prompt string) bool {
	prompt = strings.TrimSpace(prompt)
	if LooksLikeRoastPrompt(prompt) {
		return false
	}
	for _, re := range watchPatterns {
		if re.MatchString(prompt) {
			return true
		}
	}
	return false
}
