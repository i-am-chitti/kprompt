package optimize

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Rough public-cloud list-price / intensity averages for order-of-magnitude notes (T-073).
// These are NOT a bill and NOT region-accurate — always labeled as estimates.
const (
	estUSDPerVCPUHour     = 0.04  // ~$30/vCPU-month
	estUSDPerGiBHour      = 0.005 // ~$3.6/GiB-month
	estGramsCO2PerVCPUHour = 25.0 // rough compute intensity
	estGramsCO2PerGiBHour  = 3.0
)

// ApplyCostNotes attaches optional $/carbon estimate notes to idle and
// rightsizing signals when Prometheus-backed findings and request quantities exist.
// Skipped Prom sections produce no fake costs.
func ApplyCostNotes(rep *Report, window time.Duration) {
	if rep == nil {
		return
	}
	hours := window.Hours()
	if hours <= 0 {
		hours = parseWindowHours(rep.Window)
	}
	if hours <= 0 {
		hours = 1
	}

	byKey := indexWorkloads(rep.Workloads)
	var totalUSDPerHour, totalGramsPerHour float64
	var annotated int

	for i := range rep.Idle {
		w := &rep.Idle[i]
		wl, ok := byKey[workloadKey(w.Kind, w.Namespace, w.Name)]
		if !ok {
			continue
		}
		usdH, gH, note := idleEstimate(wl, *w, hours)
		if note == "" {
			continue
		}
		w.CostNote = note
		w.Message = appendEstimate(w.Message, note)
		totalUSDPerHour += usdH
		totalGramsPerHour += gH
		annotated++
	}

	for i := range rep.Rightsizing {
		d := &rep.Rightsizing[i]
		if d.Field != "request" || d.Direction != "lower" {
			continue
		}
		wl, ok := byKey[workloadKey(d.Kind, d.Namespace, d.Name)]
		if !ok {
			continue
		}
		usdH, gH, note := rightsizingLowerEstimate(wl, *d, hours)
		if note == "" {
			continue
		}
		d.CostNote = note
		d.Message = appendEstimate(d.Message, note)
		totalUSDPerHour += usdH
		totalGramsPerHour += gH
		annotated++
	}

	if annotated == 0 {
		return
	}

	// Mirror notes onto matching findings messages.
	for i := range rep.Findings {
		f := &rep.Findings[i]
		switch f.Code {
		case "optimize.idle.workload":
			for _, w := range rep.Idle {
				if f.Namespace == w.Namespace && f.Resource == fmt.Sprintf("%s/%s", w.Kind, w.Name) && w.CostNote != "" {
					f.Message = appendEstimate(f.Message, w.CostNote)
					break
				}
			}
		case "optimize.rightsizing.delta":
			for _, d := range rep.Rightsizing {
				if f.Namespace == d.Namespace && f.Resource == fmt.Sprintf("%s/%s", d.Kind, d.Name) &&
					strings.Contains(f.Title, d.Direction) && strings.Contains(f.Title, d.Resource) &&
					d.CostNote != "" && d.Field == "request" {
					f.Message = appendEstimate(f.Message, d.CostNote)
					break
				}
			}
		}
	}

	dayUSD := totalUSDPerHour * 24
	rep.Findings = append(rep.Findings, Finding{
		Code:     "optimize.cost.notes",
		Severity: SeverityInfo,
		Title:    "Cost / carbon estimates",
		Message: fmt.Sprintf(
			"Rough order-of-magnitude across %d idle/rightsizing signals: ~$%.2f/day and ~%.0f gCO2e/h if unused reserve were fully reclaimed. Not a cloud bill — generic public-cloud averages; confirm with your provider before acting.",
			annotated, dayUSD, totalGramsPerHour,
		),
	})
	if !strings.Contains(rep.Summary, "cost/carbon") {
		rep.Summary += " Includes optional cost/carbon estimate notes (labeled, not a bill)."
	}
}

func idleEstimate(wl Workload, idle IdleWorkload, windowHours float64) (usdPerHour, gramsPerHour float64, note string) {
	replicas := float64(wl.Replicas)
	if replicas < 1 {
		replicas = 1
	}
	var usdH, gH float64
	var parts []string

	if idle.CPUOfRequestPct != nil {
		cores := quantityCores(wl.CPURequest)
		if cores > 0 {
			unusedFrac := 1 - (*idle.CPUOfRequestPct / 100)
			if unusedFrac < 0 {
				unusedFrac = 0
			}
			wasteCores := cores * replicas * unusedFrac
			u := wasteCores * estUSDPerVCPUHour
			g := wasteCores * estGramsCO2PerVCPUHour
			usdH += u
			gH += g
			parts = append(parts, fmt.Sprintf("~%.2f unused vCPU", wasteCores))
		}
	}
	if idle.MemoryOfRequestPct != nil {
		bytes := quantityBytes(wl.MemoryRequest)
		if bytes > 0 {
			unusedFrac := 1 - (*idle.MemoryOfRequestPct / 100)
			if unusedFrac < 0 {
				unusedFrac = 0
			}
			wasteGiB := (bytes / (1024 * 1024 * 1024)) * replicas * unusedFrac
			u := wasteGiB * estUSDPerGiBHour
			g := wasteGiB * estGramsCO2PerGiBHour
			usdH += u
			gH += g
			parts = append(parts, fmt.Sprintf("~%.2f GiB unused reserve", wasteGiB))
		}
	}
	if usdH <= 0 && gH <= 0 {
		return 0, 0, ""
	}
	_ = windowHours // notes normalize to /day and /h for readability
	note = fmt.Sprintf(
		"estimate ~$%.2f/day unused reserve (%s; not a bill) · ~%.0f gCO2e/h rough intensity",
		usdH*24, strings.Join(parts, ", "), gH,
	)
	return usdH, gH, note
}

func rightsizingLowerEstimate(wl Workload, d RightsizingDelta, windowHours float64) (usdPerHour, gramsPerHour float64, note string) {
	replicas := float64(wl.Replicas)
	if replicas < 1 {
		replicas = 1
	}
	cur := quantityCores(d.Current)
	sug := quantityCores(d.Suggested)
	var usdH, gH float64
	var detail string

	switch d.Resource {
	case "cpu":
		if cur <= 0 || sug <= 0 || sug >= cur {
			return 0, 0, ""
		}
		delta := (cur - sug) * replicas
		usdH = delta * estUSDPerVCPUHour
		gH = delta * estGramsCO2PerVCPUHour
		detail = fmt.Sprintf("~%.2f vCPU request delta", delta)
	case "memory":
		curB := quantityBytes(d.Current)
		sugB := quantityBytes(d.Suggested)
		if curB <= 0 || sugB <= 0 || sugB >= curB {
			return 0, 0, ""
		}
		deltaGiB := ((curB - sugB) / (1024 * 1024 * 1024)) * replicas
		usdH = deltaGiB * estUSDPerGiBHour
		gH = deltaGiB * estGramsCO2PerGiBHour
		detail = fmt.Sprintf("~%.2f GiB request delta", deltaGiB)
	default:
		return 0, 0, ""
	}
	if usdH <= 0 {
		return 0, 0, ""
	}
	_ = windowHours
	note = fmt.Sprintf(
		"estimate ~$%.2f/day if request lowered (%s; not a bill) · ~%.0f gCO2e/h rough intensity",
		usdH*24, detail, gH,
	)
	return usdH, gH, note
}

func appendEstimate(message, note string) string {
	message = strings.TrimSpace(message)
	note = strings.TrimSpace(note)
	if note == "" {
		return message
	}
	if message == "" {
		return note
	}
	if strings.Contains(message, note) {
		return message
	}
	return message + " — " + note
}

func workloadKey(kind, ns, name string) string {
	return strings.ToLower(kind) + "/" + ns + "/" + name
}

func indexWorkloads(workloads []Workload) map[string]Workload {
	out := make(map[string]Workload, len(workloads))
	for _, wl := range workloads {
		out[workloadKey(wl.Kind, wl.Namespace, wl.Name)] = wl
	}
	return out
}

func parseWindowHours(label string) float64 {
	label = strings.TrimSpace(strings.ToLower(label))
	if label == "" {
		return 0
	}
	var n float64
	var unit string
	_, err := fmt.Sscanf(label, "%f%s", &n, &unit)
	if err != nil || n <= 0 {
		return 0
	}
	switch unit {
	case "h", "hr", "hrs", "hour", "hours":
		return n
	case "m", "min", "mins", "minute", "minutes":
		return n / 60
	case "d", "day", "days":
		return n * 24
	case "s", "sec", "secs", "second", "seconds":
		return n / 3600
	default:
		if d, err := time.ParseDuration(label); err == nil {
			return d.Hours()
		}
	}
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return 0
	}
	return 0
}
