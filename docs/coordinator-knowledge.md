# Coordinator Shared Knowledge

Cross-namespace **handoff memory** on the thin Coordinator — edges from recent handoffs, optionally **restart-safe**.

| Surface | How | Role |
|---------|-----|------|
| Handoff merge | `POST /v1/handoff` → `CoordinatorReply` | Origin + optional suspect probe |
| Recent ring | `GET /v1/recent` | Last N handoffs |
| Knowledge summary | `GET /v1/knowledge` | Namespace edges + latest summaries |
| Blast-radius MVP | `GET /v1/blast-radius` | Risk-ranked hops from those edges (AG-066) |
| Durable store (AG-060) | `--knowledge-backend file\|configmap` | Survive Coordinator restarts |
| CLI | `knowledge` · `blast-radius` | Human-readable views |

```bash
# Run Coordinator with durable ConfigMap store (Helm default)
kprompt agent coordinator --addr :9090 --probe-kube \
  --knowledge-backend configmap --in-cluster --knowledge-namespace kprompt-system

# Laptop file backend
kprompt agent coordinator --addr :9090 --knowledge-backend file

# Inspect
kprompt agent coordinator knowledge --url http://127.0.0.1:9090
kprompt agent coordinator blast-radius --url http://127.0.0.1:9090
kprompt agent coordinator blast-radius --url http://127.0.0.1:9090 -n payments --json
```

Helm (`charts/kprompt-coordinator`): `knowledge.enabled=true` (default) writes ConfigMap `kprompt-coordinator-knowledge` in the release namespace.

Kind demo: `make coordinator-e2e` in [kprompt-examples](https://github.com/kprompt/kprompt-examples) asserts `/v1/knowledge` durable + restore after restart (AG-061).

## What this is

1. Namespaces observed on handoffs
2. `from → suspect` edges with counts
3. Latest merged InvestigationReport summaries
4. `durable: true` when a Store is configured (file/ConfigMap)
5. Blast-radius MVP hops (`/v1/blast-radius`) with low/medium/high risk from handoff counts

Still **no Coordinator mutate** ([ADR-0017](https://github.com/kprompt/kprompt-architecture/blob/main/decisions/ADR-0017-coordinator.md)).

## What this is not

- Continuous mesh / OTel call graph product
- Full continuous blast-radius beyond handoff hops (MVP is AG-066 `/v1/blast-radius`)
- Replacement for per-namespace Incident Memory ([agent.md](./agent.md))
- Replacement for read-only Knowledge Graph MVP ([graph.md](./graph.md))
- Cluster-wide Secret value topology

## Related

- Coordinator ops: [agent-ops.md](./agent-ops.md)
- Namespace Agent modes: [namespace-agent.md](./namespace-agent.md)
- Simulation / plan blast radius: [simulation.md](./simulation.md)
- Impact reverse deps: [impact.md](./impact.md)
