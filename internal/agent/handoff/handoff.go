// Package handoff sends CoordinatorHandoff envelopes from a Namespace Agent (AG-036 / ADR-0017).
//
// Ns agents never invent foreign-namespace facts; they ask the Coordinator to verify.
package handoff

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kprompt/kprompt/internal/incident"
)

const (
	APIVersion    = "kprompt.io/v1"
	Kind          = "CoordinatorHandoff"
	SchemaVersion = "1"
)

// Envelope is the ns → Coordinator handoff document.
type Envelope struct {
	APIVersion       string                       `json:"apiVersion"`
	Kind             string                       `json:"kind"`
	SchemaVersion    string                       `json:"schemaVersion"`
	FromNamespace    string                       `json:"fromNamespace"`
	SuspectNamespace string                       `json:"suspectNamespace,omitempty"`
	Reason           string                       `json:"reason"`
	Urgency          string                       `json:"urgency,omitempty"`
	CreatedAt        time.Time                    `json:"createdAt"`
	Report           incident.InvestigationReport `json:"report"`
}

// New builds a schema-stamped handoff around an InvestigationReport v2.
func New(fromNS, suspectNS, reason string, report incident.InvestigationReport) Envelope {
	return Envelope{
		APIVersion:       APIVersion,
		Kind:             Kind,
		SchemaVersion:    SchemaVersion,
		FromNamespace:    strings.TrimSpace(fromNS),
		SuspectNamespace: strings.TrimSpace(suspectNS),
		Reason:           strings.TrimSpace(reason),
		CreatedAt:        time.Now().UTC(),
		Report:           report,
	}
}

// Validate checks envelope + embedded report.
func Validate(e Envelope) error {
	if e.Kind != Kind {
		return fmt.Errorf("handoff: kind must be %s", Kind)
	}
	if strings.TrimSpace(e.FromNamespace) == "" {
		return fmt.Errorf("handoff: fromNamespace is required")
	}
	if strings.TrimSpace(e.Reason) == "" {
		return fmt.Errorf("handoff: reason is required")
	}
	if err := incident.ValidateInvestigationReport(e.Report); err != nil {
		return fmt.Errorf("handoff: report: %w", err)
	}
	return nil
}

// Client delivers handoffs.
type Client interface {
	Handoff(ctx context.Context, env Envelope) error
}

// NopClient discards handoffs (tests / disabled).
type NopClient struct{}

func (NopClient) Handoff(context.Context, Envelope) error { return nil }

// HTTPClient POSTs JSON envelopes to a Coordinator URL.
type HTTPClient struct {
	URL        string
	HTTPClient *http.Client
}

func (c *HTTPClient) Handoff(ctx context.Context, env Envelope) error {
	if c == nil || strings.TrimSpace(c.URL) == "" {
		return fmt.Errorf("handoff: URL is required")
	}
	if err := Validate(env); err != nil {
		return err
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return err
	}
	hc := c.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		return fmt.Errorf("handoff: HTTP %d", res.StatusCode)
	}
	return nil
}

// NeedsHandoff is a cheap heuristic: report Unknowns or root/summary mention another namespace.
func NeedsHandoff(fromNS string, report incident.InvestigationReport) (suspect string, reason string, ok bool) {
	fromNS = strings.TrimSpace(fromNS)
	blob := strings.ToLower(report.Summary + " " + report.RootCauseHint() + " " + strings.Join(report.Unknowns, " "))
	for _, u := range report.Unknowns {
		lu := strings.ToLower(u)
		if strings.Contains(lu, "outside") || strings.Contains(lu, "other namespace") || strings.Contains(lu, "cross-namespace") {
			return "", "dependency may be outside my namespace — need Coordinator verification", true
		}
	}
	if strings.Contains(blob, "other namespace") || strings.Contains(blob, "outside namespace") || strings.Contains(blob, "cross-namespace") {
		return "", "suspect dependency outside namespace", true
	}
	// Explicit "namespace X" patterns are left to AG-037 routing; MVP only flags need.
	_ = fromNS
	return "", "", false
}
