# kprompt-coordinator Helm chart

Thin **Coordinator** HTTP fan-in for [kprompt](https://github.com/kprompt/kprompt) Namespace Agents ([ADR-0017](https://github.com/kprompt/kprompt-architecture/blob/main/decisions/ADR-0017-coordinator.md) · AG-037…AG-039).

```text
kprompt agent coordinator --addr :9090
```

**Never mutates workloads.** Receives `CoordinatorHandoff`, returns `CoordinatorReply` with merged InvestigationReport v2.

## Install

```bash
helm upgrade --install kprompt-coordinator ./charts/kprompt-coordinator \
  -n kprompt-system --create-namespace \
  --set image.tag=<tag>
```

Ns agents point at the Service:

```bash
# on each Namespace Agent
--coordinator-url http://kprompt-coordinator.kprompt-system.svc:9090/v1/handoff
```

## RBAC (AG-039)

| Subject | Scope | Notes |
|---------|-------|-------|
| Namespace Agent | **Role** in watch ns | Unchanged — never ClusterRole-by-default |
| Coordinator | **ServiceAccount only** by default | No ClusterRole; no get/list/watch pods/Secrets cluster-wide |
| Optional ClusterRole | **off** (`rbac.clusterRole.create=false`) | Reserved for future read-only status probes — still no mutate verbs |

Do not grant the Coordinator `create/update/patch/delete` on workloads.

## Values

| Key | Default | Notes |
|-----|---------|-------|
| `image.repository` | `ghcr.io/kprompt/kprompt` | Same binary as the ns agent |
| `service.port` | `9090` | HTTP API |
| `rbac.clusterRole.create` | `false` | Keep minimal |

See [docs/namespace-agent.md](../../docs/namespace-agent.md) · [docs/agent-ops.md](../../docs/agent-ops.md).
