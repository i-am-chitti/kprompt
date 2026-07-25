# investigate (S-002)

On-demand multi-hop RCA that emits an ADR-0014 **`Investigation`** document — not chat scroll.

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

## Honest gaps (`degraded`)

MVP lists `ingress`, `mesh`, and `prometheus` in `Investigation.degraded` — those hops are not walked yet (S-003/S-004 and later slices).

## vs `explain`

| | `explain` | `investigate` |
|--|-----------|----------------|
| Focus | Deployment → Pods → Events → Logs | + Service / Endpoints ahead of that chain |
| Artifact | explain-lite JSON | `Investigation` (`kprompt.io/v1`) |
| Trigger | “why is X crashing” | “investigate X” / root cause / RCA |

Try against [kprompt-examples](https://github.com/kprompt/kprompt-examples):

```bash
make break SCENARIO=01-crashloop
kprompt "investigate api" -n payments
```
