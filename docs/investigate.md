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

MVP lists `ingress`, `mesh`, and `prometheus` in `Investigation.degraded` — those hops are not walked yet (S-004 and later slices).

## vs `explain` / `why`

| | `explain` | `why` | `investigate` |
|--|-----------|-------|----------------|
| Focus | Deployment → Pods → Events → Logs | Cause tree on one pod/workload | + Service / Endpoints ahead of that chain |
| Artifact | explain-lite JSON | `Investigation` (`kprompt.io/v1`) | `Investigation` (`kprompt.io/v1`) |
| Trigger | generic diagnosis | “why is X pending/crashing” | “investigate X” / root cause / RCA |

See also [docs/why.md](./why.md).

Try against [kprompt-examples](https://github.com/kprompt/kprompt-examples):

```bash
make break SCENARIO=01-crashloop
kprompt "investigate api" -n payments
```
