# why (S-003)

On-demand **causal state chain** for Pending / CrashLoop / ImagePull / OOM — structured symptom → proximate → root, not chat.

Emits the same ADR-0014 **`Investigation`** artifact as [`investigate`](./investigate.md).

When findings are actionable, `why` may offer a **reviewable follow-up plan** (same gate as `explain`): TTY `y/N` or `--approve`. Nothing applies silently.

| Finding | Suggested plan (when evidence is strong) |
|---------|------------------------------------------|
| OOMKilled / memory limit | Raise Deployment memory limit |
| CrashLoopBackOff with ≥2 ReplicaSet revisions | `rollout undo` previous revision |
| ImagePullBackOff | Image patch **only** if the prompt names a replacement (`set … image to …`) |

Otherwise you get prompt-only hints (logs / describe) — never an invented image tag.

## Usage

```bash
kprompt "why is ledger Pending" -n payments
kprompt "why is api crashing" -n payments
kprompt "why is worker ImagePullBackOff" -n payments
# with a named fix:
kprompt "why is worker ImagePullBackOff — set worker image to ghcr.io/example/worker:1.2.3" -n payments
kprompt "why is api crashing" -n payments -o json   # Investigation JSON (plan gate is text/TTY path)
```

Findings are ordered:

1. **Symptom.*** (e.g. `Symptom.Pending`, `Symptom.CrashLoop`)
2. **Cause.*** proximate then root (PVC missing, ImagePullBackOff, OOMKilled, affinity/taints, …)

## vs `explain` / `investigate`

| | `explain` | `why` | `investigate` |
|--|-----------|-------|----------------|
| Focus | Deployment → Pods → Events → Logs | Cause tree on one pod/workload | + Service / Endpoints multi-hop |
| Artifact | explain-lite JSON | `Investigation` | `Investigation` |
| Suggest → approve | OOM / CrashLoop rollback / named image | Same suggest path | Same suggest path |
| Trigger | generic diagnosis | “why is X pending/crashing/oom” | “investigate X” / RCA |

## Honest gaps (`degraded`)

MVP lists `mesh` and `prometheus` in `Investigation.degraded` — metrics/mesh hops are not walked yet.

Try against [kprompt-examples](https://github.com/kprompt/kprompt-examples):

```bash
make break SCENARIO=05-pending-pvc
kprompt "why is ledger Pending" -n payments

make break SCENARIO=01-crashloop
kprompt "why is api crashing" -n payments

make break SCENARIO=02-image-pull
kprompt "explain why worker is not ready" -n payments
```
