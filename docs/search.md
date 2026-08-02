# Search / inventory query

`search` is a structured inventory query (S-010) — not a SQL or CEL REPL:

```bash
kprompt "find every Deployment using redis" -n payments
kprompt "search for postgres" -n shop
kprompt "which deployments use redis" -n payments
kprompt "find every Service using redis" -n payments --output json
```

Natural language is compiled to a **typed query** (`params.query` + optional kind /
field filters), then matched against live cluster inventory.

## MVP match fields

| Field | What it scans |
|-------|----------------|
| `name` | Resource name |
| `label` | Labels (and Service selectors / pod-template labels) |
| `annotation` | Annotations |
| `image` | Container / initContainer images |
| `env` | Env names/values and `envFrom` ConfigMap/Secret refs |
| `command` | Container command / args |

Default kinds when unspecified: **Deployment** (via intent heuristics). You can
narrow with `Deployment`, `StatefulSet`, `DaemonSet`, `Pod`, or `Service`.
Optional `params.match` limits which fields are scanned (`image`, `env`, …).

```bash
kprompt "find every Deployment using redis" -n payments --output json | jq '.result'
```

JSON `result` is a `SearchReport` with `hits[]` (`kind`, `name`, `namespace`,
`field`, `detail`). Human output is a table.

## Honest limits

- Not an arbitrary query language — no CEL, SQL, jq, or JSONPath engine in v1.
- Does **not** read Secret *values* (only SecretRef *names* via envFrom).
- List pages are capped (`DefaultReadLimit`); very large namespaces may truncate.
- Forbidden list APIs appear under `degraded`.
- “Find unused …” stays **cleanup**, not search.

`search` never mutates and never asks for approval.

See also: [Cleanup](./cleanup.md) · [Impact](./impact.md) · [Kubernetes reads](./kubernetes-reads.md).
