# Coordinator Shared Knowledge (MVP)

Cross-namespace **handoff memory** on the thin Coordinator — not a durable cluster knowledge graph.

| Surface | How | Role |
|---------|-----|------|
| Handoff merge | `POST /v1/handoff` → `CoordinatorReply` | Origin + optional suspect probe |
| Recent ring | `GET /v1/recent` | Last N handoffs (in-memory) |
| Knowledge summary | `GET /v1/knowledge` | Namespace edges + latest summaries |
| CLI | `kprompt agent coordinator knowledge` | Human-readable Shared Knowledge view |

```bash
# Run Coordinator (optionally with read-only suspect probe)
kprompt agent coordinator --addr :9090 --probe-kube

# After handoffs from ns agents (--coordinator-url …/v1/handoff):
kprompt agent coordinator knowledge --url http://127.0.0.1:9090
kprompt agent coordinator knowledge --url http://127.0.0.1:9090 --json
kprompt agent coordinator recent --url http://127.0.0.1:9090
```

## What this is

A named **Shared Knowledge MVP** over the Coordinator’s restart-lossy ring:

1. Namespaces observed on handoffs
2. `from → suspect` edges with counts
3. Latest merged InvestigationReport summaries
4. Always `durable: false` in the JSON payload

Still **no Coordinator mutate** ([ADR-0017](https://github.com/kprompt/kprompt-architecture/blob/main/decisions/ADR-0017-coordinator.md)).

## What this is not

- Durable / ConfigMap / etcd shared knowledge store
- Continuous blast-radius product graph across the whole cluster
- Replacement for per-namespace Incident Memory ([agent.md](./agent.md))
- Replacement for read-only Knowledge Graph MVP ([graph.md](./graph.md))

## Related

- Coordinator ops: [agent-ops.md](./agent-ops.md)
- Namespace Agent modes: [namespace-agent.md](./namespace-agent.md)
- Simulation / plan blast radius: [simulation.md](./simulation.md)
- Impact reverse deps: [impact.md](./impact.md)
