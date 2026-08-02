package tools

import (
	"github.com/kprompt/kprompt/internal/tools/argo"
	"github.com/kprompt/kprompt/internal/tools/certmanager"
	"github.com/kprompt/kprompt/internal/tools/crossplane"
	"github.com/kprompt/kprompt/internal/tools/gateway"
	"github.com/kprompt/kprompt/internal/tools/gitops"
	"github.com/kprompt/kprompt/internal/tools/istio"
	"github.com/kprompt/kprompt/internal/tools/keda"
	"github.com/kprompt/kprompt/internal/tools/linkerd"
	"github.com/kprompt/kprompt/internal/tools/tekton"
)

// MissingHint returns an actionable message when a backend is not available.
func MissingHint(id ID) string {
	switch id {
	case IDHelm:
		return "Helm is not available. Plan host install: kprompt setup --profile minimal (or https://helm.sh/docs/intro/install/). Kubernetes shortcut: kprompt \"deploy redis\""
	case IDArgoWorkflows:
		return argo.InstallHint()
	case IDTekton:
		return tekton.InstallHint()
	case IDKEDA:
		return keda.InstallHint()
	case IDIstio:
		return istio.InstallHint()
	case IDLinkerd:
		return linkerd.InstallHint()
	case IDGatewayAPI:
		return gateway.InstallHint()
	case IDCertManager:
		return certmanager.InstallHint()
	case IDCrossplane:
		return crossplane.InstallHint()
	case IDGitOps:
		return gitops.InstallHint()
	case IDPrometheus:
		return "Prometheus is not configured. Point at an existing URL (KPROMPT_PROMETHEUS_URL / tools.prometheus.url) or plan a stack install: kprompt setup --profile platform --only prometheus"
	case IDGrafana:
		return "Grafana is not configured. Set KPROMPT_GRAFANA_URL (and API key when required), or see config steps: kprompt setup --profile full --only grafana (config-lane only — never auto-writes)"
	case IDOpenTelemetry:
		return "Trace backend is not configured. Set KPROMPT_OTEL_ENDPOINT + KPROMPT_OTEL_BACKEND=jaeger|tempo (or tools.otel.*), or see: kprompt setup --profile full --only opentelemetry (config-lane only)"
	case IDKubernetes:
		return "Kubernetes is not reachable. Check kubeconfig and context (kubectl config current-context). setup does not create clusters."
	default:
		return "Requested tool integration is not available."
	}
}
