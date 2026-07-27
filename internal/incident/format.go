package incident

import (
	"fmt"
	"io"
	"strings"
)

// FormatReportText writes a concise InvestigationReport for humans / Slack (AG-031).
// Sections: Facts, Reasoning, Confidence, Recommendations, Unknowns (ADR-0016 §10).
func FormatReportText(w io.Writer, r InvestigationReport) error {
	if w == nil {
		return fmt.Errorf("format report: writer is nil")
	}
	var b strings.Builder
	sev := strings.TrimSpace(r.Severity)
	if sev != "" {
		fmt.Fprintf(&b, "[%s] ", strings.ToUpper(sev))
	}
	fmt.Fprintf(&b, "%s\n", strings.TrimSpace(r.Summary))
	if ns := strings.TrimSpace(r.Namespace); ns != "" {
		fmt.Fprintf(&b, "Namespace: %s\n", ns)
	}
	fmt.Fprintf(&b, "Confidence: %.2f\n\n", r.Confidence)

	if facts := strings.TrimSpace(r.Facts); facts != "" {
		b.WriteString("Facts\n")
		b.WriteString(facts)
		b.WriteString("\n\n")
	}

	if len(r.Evidence) > 0 {
		b.WriteString("Evidence\n")
		for _, e := range r.Evidence {
			line := strings.TrimSpace(e.Reason)
			if msg := strings.TrimSpace(e.Message); msg != "" {
				if line != "" {
					line += ": "
				}
				line += msg
			}
			if line == "" {
				line = e.Type
			}
			fmt.Fprintf(&b, "- (%s) %s\n", e.Type, line)
		}
		b.WriteByte('\n')
	}

	if len(r.Timeline) > 0 {
		b.WriteString("Timeline\n")
		for _, e := range r.Timeline {
			msg := firstNonEmptyReport(e.Message, e.Reason, e.Type)
			fmt.Fprintf(&b, "- %s\n", msg)
		}
		b.WriteByte('\n')
	}

	if reasoning := strings.TrimSpace(r.Reasoning); reasoning != "" {
		b.WriteString("Reasoning\n")
		b.WriteString(reasoning)
		b.WriteString("\n\n")
	}

	if len(r.Hypotheses) > 0 {
		b.WriteString("Hypotheses\n")
		for _, h := range r.Hypotheses {
			mark := " "
			if h.Primary {
				mark = "*"
			}
			fmt.Fprintf(&b, "%s %s (%.2f)\n", mark, h.Statement, h.Confidence)
			if len(h.CausalChain) > 0 {
				fmt.Fprintf(&b, "  chain: %s\n", strings.Join(h.CausalChain, " → "))
			}
		}
		b.WriteByte('\n')
	}

	fmt.Fprintf(&b, "Confidence\n%.2f\n\n", r.Confidence)

	if len(r.RecommendedActions) > 0 {
		b.WriteString("Recommendations\n")
		for i, a := range r.RecommendedActions {
			fmt.Fprintf(&b, "%d. %s", i+1, a.Title)
			if a.Confidence > 0 {
				fmt.Fprintf(&b, " (%.2f)", a.Confidence)
			}
			b.WriteByte('\n')
			if why := strings.TrimSpace(a.Why); why != "" {
				fmt.Fprintf(&b, "   why: %s\n", why)
			}
			if risk := strings.TrimSpace(a.Risk); risk != "" {
				fmt.Fprintf(&b, "   risk: %s\n", risk)
			}
			if impact := strings.TrimSpace(a.ExpectedImpact); impact != "" {
				fmt.Fprintf(&b, "   impact: %s\n", impact)
			}
			if rb := strings.TrimSpace(a.Rollback); rb != "" {
				fmt.Fprintf(&b, "   rollback: %s\n", rb)
			}
		}
		b.WriteByte('\n')
	}

	if len(r.Risks) > 0 {
		b.WriteString("Risks\n")
		for _, risk := range r.Risks {
			fmt.Fprintf(&b, "- %s\n", risk)
		}
		b.WriteByte('\n')
	}

	if len(r.Unknowns) > 0 || len(r.Degraded) > 0 {
		b.WriteString("Unknowns\n")
		for _, u := range r.Unknowns {
			fmt.Fprintf(&b, "- %s\n", u)
		}
		for _, d := range r.Degraded {
			fmt.Fprintf(&b, "- degraded: %s\n", d)
		}
		b.WriteByte('\n')
	}

	_, err := io.WriteString(w, b.String())
	return err
}

func firstNonEmptyReport(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
