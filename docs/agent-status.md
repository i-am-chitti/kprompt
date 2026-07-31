# Namespace Agent intelligence brief (MVP)

Read-only rollup of health, open incidents, patterns, and memory deps for one
namespace — **not** continuous multi-signal reasoning beyond stored evidence.

```bash
kprompt agent status -n payments
kprompt agent status -n payments --json

# In-cluster ConfigMap stores (Observe Helm defaults)
kprompt agent status -n payments \
  --incidents-backend configmap \
  --patterns-backend configmap \
  --memory-backend configmap \
  --in-cluster
```

| Field | Source |
|-------|--------|
| Health score / trend / podReady | AG-011 tracker + live pods when kube client available |
| Open incidents | Durable correlate store (AG-032) |
| Patterns | Incident Memory patterns (AG-016 · AG-054) |
| Memory deps | Namespace memory facts (AG-015) |

## Detector catalog (AG-065)

Heuristic catalog also includes **ResourceQuota exceeded** and **HPA metrics failure**
alongside OOM, ImagePull, CrashLoop, Pending, probe, rollout, DNS, storage (AG-026).

## What this is not

- Autopilot apply
- LLM-required briefing
- Multi-cluster fleet SaaS
- Continuous reasoning that invents foreign-namespace facts

## Related

- Fleet inventory: [agent-fleet.md](./agent-fleet.md)
- Modes: [namespace-agent.md](./namespace-agent.md)
- Observe agent: [agent.md](./agent.md)
