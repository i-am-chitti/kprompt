# investigate (S-002)

On-demand multi-hop RCA that emits an ADR-0014 **`Investigation`** document — not chat scroll.

**Shape of the work:** investigate is one path through the **Investigation Graph** — signal hops → findings → optional PlanResult → approve → apply → verify. See [investigation-graph.md](./investigation-graph.md) ([S-017](https://github.com/kprompt/kprompt-architecture/issues/209)).

## Usage

```bash
kprompt "investigate api" -n payments
kprompt "investigate api" -n payments -o json
```

Walks (MVP):

1. **Service** (selectors matching the workload)
2. **Endpoints** (ready / notReady counts)
3. **Deployment → ReplicaSet → Pods** (T-024 explain chain)
4. **Events + Logs** (worst pod)

Root cause + confidence come from findings (CrashLoop / ImagePull / OOM / no ready endpoints). Optional suggested fix still goes through PlanResult → approve (never auto-apply).

Prefer a **loop** (this sequential walk) for one Service/workload. Prefer graph width (fan-out / Coordinator) when signals or namespaces are independent — see [investigation-graph.md](./investigation-graph.md#loop-vs-graph). Confidence and suggested fixes are still bound by [reality anchors](./reality-anchors.md) (hard deny, EvidenceRef, PlanResult — not chat vibes). **Pre-trust (T-089):** after the walk, `internal/pretrust` clamps high confidence without EvidenceRef / contradicting re-read and can withhold approve UX for suggested fixes.

## Honest gaps (`degraded`)

MVP lists `ingress`, `mesh`, and `prometheus` in `Investigation.degraded` — those hops are not walked yet (S-004 and later slices).

## vs `explain` / `why`

| | `explain` | `why` | `investigate` |
|--|-----------|-------|----------------|
| Focus | Deployment → Pods → Events → Logs | Cause tree on one pod/workload | + Service / Endpoints ahead of that chain |
| Artifact | explain-lite JSON | `Investigation` (`kprompt.io/v1`) | `Investigation` (`kprompt.io/v1`) |
| Trigger | generic diagnosis | “why is X pending/crashing” | “investigate X” / root cause / RCA |
| Shape | short chain | **loop** (usually) | chain today; graph when fan-out lands (T-090) |

See also [docs/why.md](./why.md) · [investigation-graph.md](./investigation-graph.md).

Try against [kprompt-examples](https://github.com/kprompt/kprompt-examples):

```bash
make break SCENARIO=01-crashloop
kprompt "investigate api" -n payments
```
