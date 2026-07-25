# timeline (S-004)

Ordered **incident chronology** for a workload — Events + ReplicaSet/rollout revisions + HPA — not chat scroll.

Emits the same ADR-0014 **`Investigation`** artifact; the primary payload is `timeline[]` of `EvidenceRef` (time-sorted).

## Usage

```bash
kprompt "timeline for api" -n payments
kprompt "what happened to ledger" -n payments -o json
```

Optional window (default `1h`):

```bash
kprompt "timeline for api" -n payments
# LLM / normalize sets params.window=1h; override via structured intent when using stubs/tests
```

## Sources (MVP)

1. **Events** on the Deployment (or Pod) and owned pods
2. **ReplicaSet** revisions (`deployment.kubernetes.io/revision`)
3. **HPA** targeting the Deployment (status + condition transitions)

## Honest gaps (`degraded`)

MVP lists `prometheus`, `otel`, and `mesh` in `Investigation.degraded` — metrics/traces/mesh hops are not walked yet.

## vs `investigate` / `why`

| | `investigate` | `why` | `timeline` |
|--|---------------|-------|------------|
| Focus | Multi-hop RCA | Cause tree | Chronology |
| Primary field | `findings` | ordered Symptom→Cause | `timeline[]` |
| Trigger | “investigate X” | “why is X pending” | “timeline for X” / “what happened to X” |

Try against [kprompt-examples](https://github.com/kprompt/kprompt-examples):

```bash
make break SCENARIO=01-crashloop
kprompt "timeline for api" -n payments
```
