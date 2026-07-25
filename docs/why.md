# why (S-003)

On-demand **causal state chain** for Pending / CrashLoop / ImagePull / OOM — structured symptom → proximate → root, not chat.

Emits the same ADR-0014 **`Investigation`** artifact as [`investigate`](./investigate.md).

## Usage

```bash
kprompt "why is ledger Pending" -n payments
kprompt "why is api crashing" -n payments -o json
```

Findings are ordered:

1. **Symptom.*** (e.g. `Symptom.Pending`, `Symptom.CrashLoop`)
2. **Cause.*** proximate then root (PVC missing, ImagePullBackOff, OOMKilled, affinity/taints, …)

## vs `explain` / `investigate`

| | `explain` | `why` | `investigate` |
|--|-----------|-------|----------------|
| Focus | Deployment → Pods → Events → Logs | Cause tree on one pod/workload | + Service / Endpoints multi-hop |
| Artifact | explain-lite JSON | `Investigation` | `Investigation` |
| Trigger | generic diagnosis | “why is X pending/crashing/oom” | “investigate X” / RCA |

## Honest gaps (`degraded`)

MVP lists `mesh` and `prometheus` in `Investigation.degraded` — metrics/mesh hops are not walked yet.

Try against [kprompt-examples](https://github.com/kprompt/kprompt-examples):

```bash
make break SCENARIO=05-pending-pvc
kprompt "why is ledger Pending" -n payments
```
