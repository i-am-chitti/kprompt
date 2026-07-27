# `kprompt setup`

Detect gaps via `tools.Detect` and print a **dry-run bootstrap plan**
([ADR-0018](https://github.com/kprompt/kprompt-architecture/blob/main/decisions/ADR-0018-kprompt-setup.md) · T-062).

```bash
kprompt setup
kprompt setup --profile minimal
kprompt setup --profile platform --json
kprompt setup --context kind-dev
```

## What it covers (platform profile — default)

| Component | Lane | When needed |
|-----------|------|-------------|
| Helm | host | `helm` not on PATH |
| Argo Workflows | cluster | Workflow CRD missing |
| Prometheus | config | URL unset |

`minimal` = Helm only · `full` = platform + Grafana + OTel URL steps.

## Safety

- **Default is dry-run.** No host package installs, no cluster Helm/manifest apply.
- `--approve` is reserved for T-063 / T-064 and currently **errors after printing the plan** (honest: nothing installed).
- Cluster-lane proposals are **blocked** if Kubernetes is unreachable.
- Prefer configuring an existing Prometheus URL over installing a stack when one already exists.

## Related

- Inventory: `kprompt tools`
- Health: `kprompt doctor`
- Learn profile: `kprompt learn`
- Apply slices: T-063 (host) · T-064 (cluster) · T-065 (profiles) · T-066 (docs/website)
