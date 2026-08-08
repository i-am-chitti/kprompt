# Knowledge Graph (MVP+)

Read-only relationships across Services, Ingress, volume mounts (PVC / Secret /
ConfigMap **names**), external hosts (ExternalName + literal env hostnames),
NetworkPolicy peers, consumers, and remembered deps — **not** a Secret-value
CMDB or continuous mesh/CMDB product.

Contracts: service graph (T-059 · T-060 · AG-063 · AG-064 · RT-013…RT-016) ·
impact (S-005 · T-083) · namespace memory deps (AG-015).

## What ships today

| Surface | Prompt / CLI | Artifact |
|---------|--------------|----------|
| Service dependency graph | `kprompt "show service dependency graph" -n payments` | `type: service-graph` nodes/edges |
| Reverse impact | `kprompt "who consumes redis" -n payments` | `Investigation` + `degraded` |
| Namespace dep facts | `kprompt agent memory discover/list -n payments` | local / ConfigMap facts |
| Agent dump | `kprompt agent graph -n payments` | same `service-graph` JSON |
| Autopilot impact note | `--autopilot-propose` | `expectedImpact` cites `depends_on` / `allows` (RT-015) |

```bash
kprompt "show service dependency graph" -n payments
kprompt agent graph -n payments
kprompt agent graph -n payments --ingress --pvc --volume-refs --network-policy --external-deps
kprompt agent graph -n payments -o json
```

Helm / laptop Observe agents do **not** upload topology to `api.kprompt.ai`.

## Node & edge honesty

**Included:**

- Services, **ready** EndpointSlice-backed pods (`routes`)
- Optional NetworkPolicy `selects` + peer `allows` (podSelector / IPBlock → ExternalHost)
- **Ingress → Service** `exposes` edges (AG-063)
- **Pod → PVC** `mounts` edges (AG-063)
- **Pod → Secret / ConfigMap** `mounts` from volumes + env/envFrom — **names only**, never `Secret.data` (AG-064)
- **ExternalName Service → ExternalHost** `depends_on` (RT-013)
- **Pod literal env URL/host → ExternalHost|Service** `depends_on` (RT-013; never SecretKeyRef values)
- Optional OTel **calls** edges when a querier is configured
- Static reverse consumers via [impact.md](./impact.md)
- Heuristic redis/postgres/… facts via Incident Memory (evidence, not proof)
- AutopilotProposal `expectedImpact` may append graph depends_on/allows notes (RT-015)

**Not claimed yet (still building / exploring):**

- Reading or indexing Secret/ConfigMap **values** (credential CMDB)
- Always-on Kafka / mesh call graph without OTel
- Interactive topology UI as a full product graph editor

~~Interactive topology UI / Team `/graph` viewer~~ — shipped (A-021).
~~External host depends_on from ExternalName + env~~ — shipped (RT-013).

## Non-goals

- Auto-remediation from graph edges
- Inventing runtime callers when OTel/mesh signals are missing
- Replacing Prometheus, service mesh, or CMDB products
- Secret vault / credential CMDB (Secret.data stays out forever for this surface)
- Silent heal from topology edges
