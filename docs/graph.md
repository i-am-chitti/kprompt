# Knowledge Graph (MVP+)

Read-only relationships across Services, Ingress, volume mounts (PVC / Secret /
ConfigMap **names**), consumers, and remembered deps — **not** a Secret-value
CMDB or continuous external-API topology product.

Contracts: service graph (T-059 · T-060 · AG-063 · AG-064) · impact (S-005 · T-083) · namespace memory deps (AG-015).

## What ships today

| Surface | Prompt / CLI | Artifact |
|---------|--------------|----------|
| Service dependency graph | `kprompt "show service dependency graph" -n payments` | `type: service-graph` nodes/edges |
| Reverse impact | `kprompt "who consumes redis" -n payments` | `Investigation` + `degraded` |
| Namespace dep facts | `kprompt agent memory discover/list -n payments` | local / ConfigMap facts |
| Agent dump | `kprompt agent graph -n payments` | same `service-graph` JSON |

```bash
kprompt "show service dependency graph" -n payments
kprompt agent graph -n payments
kprompt agent graph -n payments --ingress --pvc --volume-refs --network-policy
kprompt agent graph -n payments -o json
```

Helm / laptop Observe agents do **not** upload topology to `api.kprompt.ai`.

## Node & edge honesty

**Included:**

- Services, EndpointSlice-backed pods, optional NetworkPolicy selects
- **Ingress → Service** `exposes` edges (AG-063)
- **Pod → PVC** `mounts` edges (AG-063)
- **Pod → Secret / ConfigMap** `mounts` from volumes + env/envFrom — **names only**, never `Secret.data` (AG-064)
- Optional OTel **calls** edges when a querier is configured
- Static reverse consumers via [impact.md](./impact.md)
- Heuristic redis/postgres/… facts via Incident Memory (evidence, not proof)

**Not claimed yet (still building / exploring):**

- Reading or indexing Secret/ConfigMap **values**
- Always-on cluster-wide external APIs / Kafka as first-class product nodes
- Complete mesh call graph without OTel

~~Interactive topology UI / Team `/graph` viewer~~ — shipped (A-021).

## Non-goals

- Auto-remediation from graph edges
- Inventing runtime callers when OTel/mesh signals are missing
- Replacing Prometheus, service mesh, or CMDB products
- Secret vault / external-API continuous topology product (still building)
