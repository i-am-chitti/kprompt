# Coordinator Shared Knowledge

Cross-namespace **handoff memory** on the thin Coordinator — edges from recent handoffs, optionally **restart-safe**, with an opt-in **proactive tick** (RT-009).

| Surface | How | Role |
|---------|-----|------|
| Handoff merge | `POST /v1/handoff` → `CoordinatorReply` | Origin + optional suspect probe |
| Proactive tick | `--tick-interval` (RT-009) | Re-scan Shared Knowledge + re-probe without new handoff |
| Recent ring | `GET /v1/recent` | Last N handoffs |
| Knowledge summary | `GET /v1/knowledge` | Namespace edges + latest summaries |
| Blast-radius MVP | `GET /v1/blast-radius` | Risk-ranked hops; `status=degraded` without mesh/OTel (RT-010) |
| Cascade cap | `--max-hops` (RT-011) | BFS hop limit from focus namespace |
| Durable store (AG-060) | `--knowledge-backend file\|configmap` | Survive Coordinator restarts |
| CLI | `knowledge` · `blast-radius` | Human-readable views |

```bash
# Run Coordinator with durable ConfigMap store (Helm default)
kprompt agent coordinator --addr :9090 --probe-kube \
  --knowledge-backend configmap --in-cluster --knowledge-namespace kprompt-system

# Continuous correlation (opt-in — not silent heal)
kprompt agent coordinator --addr :9090 --probe-kube --tick-interval 5m --tick-budget 5 --max-hops 3

# Laptop file backend
kprompt agent coordinator --addr :9090 --knowledge-backend file

# Inspect
kprompt agent coordinator knowledge --url http://127.0.0.1:9090
kprompt agent coordinator blast-radius --url http://127.0.0.1:9090
kprompt agent coordinator blast-radius --url http://127.0.0.1:9090 -n payments --json
```

Helm (`charts/kprompt-coordinator`): `knowledge.enabled=true` (default); set `continuous.tickInterval` (e.g. `5m`) to enable RT-009.

Kind demo: `make coordinator-e2e` in [kprompt-examples](https://github.com/kprompt/kprompt-examples) asserts `/v1/knowledge` durable + restore after restart (AG-061).

## What this is

1. Namespaces observed on handoffs
2. `from → suspect` edges with counts
3. Latest merged InvestigationReport summaries
4. `durable: true` when a Store is configured (file/ConfigMap)
5. Blast-radius MVP hops with low/medium/high risk; `status=ok|degraded` (RT-010)
6. Opt-in proactive tick refreshing edges without a new handoff POST (RT-009)
7. Audit of every merge (handoff + tick) with `mutateAttempted=false` (RT-011)

Still **no Coordinator mutate** ([ADR-0017](https://github.com/kprompt/kprompt-architecture/blob/main/decisions/ADR-0017-coordinator.md)). **Continuous ≠ silent heal.**

## What this is not

- Silent fleet remediation / Autopilot apply from the Coordinator
- Full mesh / OTel call graph product (opt-in `--mesh-otel` only flips status honesty today)
- Replacement for per-namespace Incident Memory ([agent.md](./agent.md))
- Replacement for read-only Knowledge Graph MVP ([graph.md](./graph.md))
- Cluster-wide Secret value topology

## Related

- Coordinator ops: [agent-ops.md](./agent-ops.md)
- Namespace Agent modes: [namespace-agent.md](./namespace-agent.md)
- Simulation / plan blast radius: [simulation.md](./simulation.md)
- Impact reverse deps: [impact.md](./impact.md)
- Topology KG: [graph.md](./graph.md)
