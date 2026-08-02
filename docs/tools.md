# kprompt tools

`kprompt tools` shows which integrations `kprompt` can use from your machine
and current cluster.

It probes local binaries, configured HTTP backends, and Kubernetes CRDs. The
command is **read-only** and does not call an LLM.

When a tool is missing, the `DETAIL` / hint column points at **`kprompt setup`**
for components that setup can plan (Helm, Argo Workflows, Prometheus) or at
config URLs for Grafana / OTel. See [setup.md](./setup.md).

## What it detects

The current CLI reports tools such as:

- Kubernetes
- Helm
- Argo Workflows
- Tekton
- KEDA
- Istio
- Linkerd
- Gateway API
- cert-manager
- Crossplane
- GitOps
- Prometheus
- Grafana
- OpenTelemetry

## Examples

```bash
kprompt tools
kprompt tools --json
kprompt tools --context staging
```

## Closing gaps with setup

```bash
# Dry-run plan for the default platform profile
kprompt setup

# Host Helm only
kprompt setup --profile minimal --approve

# Prometheus stack only (within platform)
kprompt setup --profile platform --only prometheus --approve
```

Honest limits: setup does **not** install Tekton/KEDA/Istio/Crossplane/GitOps,
does **not** create clusters, and does **not** auto-write Grafana/OTel config
(those are config-lane hints only).

## Output

Default output is a table:

- `TOOL`
- `STATUS`
- `DETAIL`

Use JSON for scripting or debugging:

```bash
kprompt tools --json
```

## Context override

Cluster and CRD checks use the active context by default. To inspect another
context:

```bash
kprompt tools --context prod
```

## URL and config knobs

Some integrations are enabled by URL/config rather than a local binary alone.

Common environment variables:

- `KPROMPT_PROMETHEUS_URL`
- `KPROMPT_GRAFANA_URL`
- `KPROMPT_GRAFANA_API_KEY`
- `KPROMPT_OTEL_ENDPOINT`
- `KPROMPT_OTEL_BACKEND`

Matching config keys:

- `tools.prometheus.url`
- `tools.grafana.url`
- `tools.otel.endpoint`
- `tools.otel.backend`

Example:

```bash
kprompt config set tools.prometheus.url http://prometheus.monitoring:9090
kprompt config set tools.otel.backend tempo
kprompt tools
```

If a tool is disabled or not configured, `kprompt tools` reports that in the
`DETAIL` column and may include a hint.
