# Namespace Agent fleet (MVP)

Read-only inventory of Observe / Namespace Agent surfaces in the cluster.

```bash
kprompt agent list -A
kprompt agent list -n payments
kprompt agent list -A --json
```

| Source | What |
|--------|------|
| `cr` | `KpromptAgent` custom resources (mode, watch ns, Ready, health, open incidents, last alert) |
| `deployment` | Deployments labeled `app.kubernetes.io/name=kprompt-agent` not already covered by a CR |

## What this is

A **fleet UX MVP** for operators who run many namespace agents via Operator or Helm: one command to see what’s deployed and whether status sync looks healthy.

## What this is not

- Hosted fleet SaaS / multi-cluster control plane UI
- Live log tail or Slack thread browser
- Autopilot apply across namespaces
- Richer continuous intelligence beyond CR status fields (still building)

## Related

- Observe agent: [agent.md](./agent.md)
- Operator / CRD: [agent.md](./agent.md#kpromptagent-crd-ag-013)
- Namespace Agent modes: [namespace-agent.md](./namespace-agent.md)
