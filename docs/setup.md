# `kprompt setup`

Detect gaps via `tools.Detect` and print a bootstrap plan
([ADR-0018](https://github.com/kprompt/kprompt-architecture/blob/main/decisions/ADR-0018-kprompt-setup.md)).

```bash
kprompt setup
kprompt setup --profile minimal
kprompt setup --profile minimal --approve   # install missing Helm (T-063)
kprompt setup --dry-run --json
kprompt setup --context kind-dev
```

## What it covers (platform profile — default)

| Component | Lane | When needed | Apply |
|-----------|------|-------------|-------|
| Helm | host | `helm` not on PATH | T-063 (`--approve` / TTY confirm) |
| Argo Workflows | cluster | Workflow CRD missing | plan-only (T-064) |
| Prometheus | config | URL unset | plan-only / `config set` |

`minimal` = Helm only · `full` = platform + Grafana + OTel URL steps.

## Host install OS matrix (T-063)

| OS | Method |
|----|--------|
| macOS (darwin) | Homebrew: `brew install helm` (brew must be on PATH) |
| Linux | `brew install helm` if brew exists; else official [get-helm-3](https://helm.sh/docs/intro/install/) script (`curl` required) |
| Other | Unsupported — install manually |

- **Skip** if `helm` already on PATH.
- Never silent: needs `--approve` or interactive `y`.
- Only Helm is wired for host apply in T-063.

## Safety

- **Default is dry-run** (plan only).
- `--approve` applies **host** steps only; cluster/config stay printed, not installed.
- Cluster-lane proposals are **blocked** if Kubernetes is unreachable.
- Prefer configuring an existing Prometheus URL over installing a stack.

## Related

- Inventory: `kprompt tools`
- Health: `kprompt doctor`
- Learn profile: `kprompt learn`
- Remaining: T-064 (cluster) · T-065 (profiles) · T-066 (website honesty)
