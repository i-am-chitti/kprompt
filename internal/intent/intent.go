package intent

import (
	"encoding/json"
	"strings"
)

// Kind classifies a user intent.
type Kind string

const (
	KindDeploy      Kind = "deploy"
	KindInstall     Kind = "install"
	KindUpgrade     Kind = "upgrade"
	KindScale       Kind = "scale"
	KindRollback    Kind = "rollback"
	KindGet         Kind = "get"
	KindExplain     Kind = "explain"
	KindInvestigate Kind = "investigate"
	KindWhy         Kind = "why"
	KindTimeline    Kind = "timeline"
	KindImpact      Kind = "impact"
	KindAudit       Kind = "audit"
	KindCleanup     Kind = "cleanup"
	KindSearch      Kind = "search"
	KindLearn       Kind = "learn"
	KindDrift       Kind = "drift"
	KindLogs        Kind = "logs"
	KindDescribe    Kind = "describe"
	KindWorkflow    Kind = "workflow"
	KindTekton      Kind = "tekton"
	KindKEDA        Kind = "keda"
	KindHPA         Kind = "hpa"
	KindIstio       Kind = "istio"
	KindCrossplane  Kind = "crossplane"
	KindGitOps      Kind = "gitops"
	KindPerformance Kind = "performance"
	KindTrace       Kind = "trace"
	KindDashboard   Kind = "dashboard"
	KindOptimize    Kind = "optimize"
	KindRoast       Kind = "roast"
	KindGraph       Kind = "graph"
	KindDelete      Kind = "delete"
	KindPatch       Kind = "patch"
	KindDeny        Kind = "deny"
	KindUnknown     Kind = "unknown"
)

// ExtractKinds are kinds the LLM may emit (matches SchemaJSON enum).
// KindPatch is intentionally excluded — it is produced only by suggest follow-ups.
var ExtractKinds = map[Kind]struct{}{
	KindDeploy: {}, KindInstall: {}, KindUpgrade: {}, KindScale: {}, KindRollback: {},
	KindGet: {}, KindExplain: {}, KindInvestigate: {}, KindWhy: {}, KindTimeline: {},
	KindImpact: {}, KindAudit: {}, KindCleanup: {}, KindSearch: {}, KindLearn: {}, KindDrift: {},
	KindLogs: {}, KindDescribe: {}, KindWorkflow: {}, KindTekton: {}, KindKEDA: {},
	KindHPA: {}, KindIstio: {}, KindCrossplane: {}, KindGitOps: {}, KindPerformance: {},
	KindTrace: {}, KindDashboard: {}, KindOptimize: {}, KindRoast: {}, KindGraph: {}, KindDelete: {},
	KindDeny: {}, KindUnknown: {},
}

// NormalizeKind coerces any non-extract kind to unknown so invented LLM values
// (e.g. "hpascaleup") never reach the planner as fake kinds. Case is folded so
// "HPA" maps to KindHPA.
func NormalizeKind(k Kind) Kind {
	k = Kind(strings.ToLower(strings.TrimSpace(string(k))))
	if _, ok := ExtractKinds[k]; ok {
		return k
	}
	return KindUnknown
}

// Intent is the structured result of NL understanding.
type Intent struct {
	Kind       Kind           `json:"kind"`
	Target     Target         `json:"target"`
	Context    string         `json:"context,omitempty"` // kubeconfig context from prompt
	Params     map[string]any `json:"params,omitempty"`
	Confidence float64        `json:"confidence,omitempty"`
	Raw        string         `json:"-"`
}

// Target identifies a Kubernetes object or query scope.
type Target struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Kind      string `json:"kind,omitempty"` // Deployment, Pod, ...
}

// SchemaJSON is the versioned JSON schema used for structured LLM output.
const SchemaJSON = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["kind", "target"],
  "properties": {
    "kind": {
      "type": "string",
      "enum": ["deploy", "install", "upgrade", "scale", "rollback", "get", "explain", "investigate", "why", "timeline", "impact", "audit", "cleanup", "search", "learn", "drift", "logs", "describe", "workflow", "tekton", "keda", "hpa", "istio", "crossplane", "gitops", "performance", "trace", "dashboard", "optimize", "roast", "graph", "delete", "deny", "unknown"]
    },
    "target": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "name": { "type": "string" },
        "namespace": { "type": "string" },
        "kind": { "type": "string" }
      }
    },
    "context": { "type": "string" },
    "params": {
      "type": "object",
      "additionalProperties": true
    },
    "confidence": { "type": "number" }
  }
}`

// ParseStructured validates and unmarshals model JSON into Intent.
func ParseStructured(raw []byte) (Intent, error) {
	var in Intent
	if err := json.Unmarshal(raw, &in); err != nil {
		return Intent{}, err
	}
	in.Kind = NormalizeKind(in.Kind)
	if in.Params == nil {
		in.Params = map[string]any{}
	}
	return in, nil
}

// Replicas extracts an int replicas param when present.
func (i Intent) Replicas() (int32, bool) {
	v, ok := i.Params["replicas"]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int32(n), true
	case int:
		return int32(n), true
	case int32:
		return n, true
	case json.Number:
		i64, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int32(i64), true
	default:
		return 0, false
	}
}
