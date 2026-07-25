// Package crdstatus patches KpromptAgent.status (AG-013).
//
// Full reconciliation lives in the Operator (AG-014). This helper lets the
// running agent optionally surface health + last alert onto a named CR.
package crdstatus

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	agentv1 "github.com/kprompt/kprompt/api/v1"
	"github.com/kprompt/kprompt/internal/agent/health"
	"github.com/kprompt/kprompt/internal/incident"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

var gvr = schema.GroupVersionResource{
	Group:    agentv1.Group,
	Version:  agentv1.Version,
	Resource: "kpromptagents",
}

// Config selects which CR to patch. Empty Name disables sync.
type Config struct {
	Name      string
	Namespace string
}

// FromEnv reads KPROMPT_AGENT_CR and KPROMPT_AGENT_CR_NAMESPACE
// (namespace falls back to POD_NAMESPACE / default).
func FromEnv() Config {
	ns := os.Getenv("KPROMPT_AGENT_CR_NAMESPACE")
	if ns == "" {
		ns = os.Getenv("POD_NAMESPACE")
	}
	if ns == "" {
		ns = "default"
	}
	return Config{
		Name:      os.Getenv("KPROMPT_AGENT_CR"),
		Namespace: ns,
	}
}

// Syncer patches KpromptAgent status subresource.
type Syncer struct {
	dyn dynamic.Interface
	cfg Config
}

// New returns a Syncer. If cfg.Name is empty, methods are no-ops.
func New(dyn dynamic.Interface, cfg Config) *Syncer {
	return &Syncer{dyn: dyn, cfg: cfg}
}

// Enabled reports whether a CR name is configured.
func (s *Syncer) Enabled() bool {
	return s != nil && s.cfg.Name != "" && s.dyn != nil
}

// PatchHealth updates healthScore / healthTrend / openIncidents.
func (s *Syncer) PatchHealth(ctx context.Context, snap health.Snapshot) error {
	if !s.Enabled() {
		return nil
	}
	status := map[string]any{
		"healthScore":   snap.Score,
		"healthTrend":   snap.Trend,
		"openIncidents": snap.OpenIncidents,
		"conditions": []map[string]any{
			{
				"type":               "Ready",
				"status":             "True",
				"reason":             "Observing",
				"message":            "agent is running Observe Mode",
				"lastTransitionTime": time.Now().UTC().Format(time.RFC3339),
			},
		},
	}
	return s.patchStatus(ctx, status)
}

// PatchAlert records the last gated AgentAlert.
func (s *Syncer) PatchAlert(ctx context.Context, alert incident.AgentAlert) error {
	if !s.Enabled() {
		return nil
	}
	status := map[string]any{
		"lastAlert": map[string]any{
			"incidentId": alert.IncidentID,
			"severity":   alert.Severity,
			"summary":    alert.Summary,
			"confidence": alert.Confidence,
			"status":     alert.Status,
			"at":         time.Now().UTC().Format(time.RFC3339),
		},
	}
	return s.patchStatus(ctx, status)
}

func (s *Syncer) patchStatus(ctx context.Context, status map[string]any) error {
	body, err := json.Marshal(map[string]any{
		"status": status,
	})
	if err != nil {
		return err
	}
	_, err = s.dyn.Resource(gvr).Namespace(s.cfg.Namespace).Patch(
		ctx,
		s.cfg.Name,
		types.MergePatchType,
		body,
		metav1.PatchOptions{},
		"status",
	)
	if err != nil {
		return fmt.Errorf("patch KpromptAgent/%s status: %w", s.cfg.Name, err)
	}
	return nil
}
