# `kprompt setup`

Detect gaps via `tools.Detect` and print a bootstrap plan
([ADR-0018](https://github.com/kprompt/kprompt-architecture/blob/main/decisions/ADR-0018-kprompt-setup.md)).

```bash
kprompt setup
kprompt setup --profile minimal --approve          # host Helm
kprompt setup --profile platform --approve         # Helm + Argo + Prom stack
kprompt setup --dry-run --json
kprompt setup --context kind-dev
```

## What it covers (platform profile — default)

| Component | Lane | When needed | Apply |
|-----------|------|-------------|-------|
| Helm | host | `helm` not on PATH | T-063 |
| Argo Workflows | cluster | Workflow CRD missing | T-064 (`kubectl apply` → ns `argo`) |
| Prometheus | cluster | URL unset / unavailable | T-064 (`helm install` kube-prometheus-stack → ns `monitoring`) |
| Grafana / OTel | config | URL unset (`full` profile) | manual `config set` |

## Namespace defaults (T-064)

| Operator | Namespace | Notes |
|----------|-----------|-------|
| Argo Workflows | `argo` | Manifests pinned to release `v3.6.2` |
| kube-prometheus-stack | `monitoring` | Release name `kprompt-prom` |

After Prometheus install, set:

```bash
kprompt config set tools.prometheus.url http://kprompt-prom-kube-prometheus-stack-prometheus.monitoring.svc:9090
```

## Host install OS matrix (T-063)

| OS | Method |
|----|--------|
| macOS (darwin) | Homebrew: `brew install helm` |
| Linux | brew if present, else official get-helm-3 (`curl` required) |
| Other | Unsupported — install manually |

## Safety

- **Default is dry-run** (plan only).
- Apply needs `--approve` or interactive confirm.
- Cluster path: **plan → safety.EvaluatePlan → apply** (install-only).
- **Wipe-class denied:** `helm uninstall --all`, namespace delete, etc.
- Config-lane steps (Grafana/OTel) are never auto-written.

## Related

- `kprompt tools` · `kprompt doctor` · `kprompt learn`
- Remaining: T-065 (profile flags polish) · T-066 (website honesty)
