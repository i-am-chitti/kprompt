# Explain architecture

`architecture` narrates a high-level platform shape from **learn** + **graph** +
heuristic deps (S-012) — not an LLM essay and not a CMDB.

```bash
kprompt "explain architecture" -n payments
kprompt "what does this cluster look like"
kprompt "platform overview" -n shop --output json
```

Example narrative:

> Namespace payments looks like: Gateway API + GitOps + Redis + Kafka + Prometheus.
> Graph shows 1 Ingress→Service expose edge(s), 4 Service→Pod routing edge(s).

## Signals

| Source | What it contributes |
|--------|---------------------|
| `kprompt learn` profile | Gateway API, GitOps, mesh, observability, Helm, … |
| Service graph | Services / Ingress / PVC counts + edge summary |
| Dependency discover | redis / postgres / kafka / … name+env hints |

Confidence is `high` / `medium` / `low` from how rich those signals are. A thin
profile still returns a sketch plus hints to run `kprompt learn`.

```bash
kprompt "explain architecture" -n payments --output json | jq '.result'
```

JSON `result` is an `ArchitectureNarrative` with `narrative`, `confidence`,
`components[]`, and optional `hints` / `degraded`.

## Honest limits

- Template narrative from detected signals — not generative prose.
- Learn profile missing → lower confidence + degraded note (does not invent tools).
- Redis/Kafka/… facts are **name/env heuristics**, not proof of a managed data plane.
- Service dependency graph alone stays `graph`; this command is the rollup story.

Never mutates and never asks for approval.

See also: [Learn](./learn.md) · [Graph](./graph.md).
