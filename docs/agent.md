# kprompt Observe agent

In-cluster, **namespace-scoped** agent that continuously watches Pods/Events, correlates incidents, optionally analyzes with an LLM, and notifies Slack/webhooks.

This is **Observe Mode only** — it never applies, patches, or deletes cluster resources ([ADR-0013](https://github.com/kprompt/kprompt-architecture/blob/main/decisions/ADR-0013-in-cluster-agent.md)).

## Laptop smoke test

```bash
kprompt agent run -n payments --analyze --fetch-logs --health --heuristic
kprompt agent run -n payments --slack --fetch-logs   # needs Slack env
```

## Helm install (AG-012)

Chart path: [`charts/kprompt-agent`](../charts/kprompt-agent).

```bash
docker build -t ghcr.io/kprompt/kprompt:dev .
# push to a registry your cluster can pull

kubectl -n payments create secret generic kprompt-agent \
  --from-literal=OPENAI_API_KEY="$OPENAI_API_KEY"

helm upgrade --install kprompt-agent ./charts/kprompt-agent \
  -n payments --create-namespace \
  --set image.tag=dev \
  --set agent.heuristic=false
```

RBAC is a **Role** in the watch namespace (pods, events, logs, deployments, … — get/list/watch only).

## KpromptAgent CRD (AG-013)

CRD installs with the Helm chart (`charts/kprompt-agent/crds/`). Standalone:

```bash
kubectl apply -f deploy/crd/kprompt.ai_kpromptagents.yaml
kubectl apply -f config/samples/kpromptagent.yaml
```

`spec.mode` defaults to **Observe**. Status fields: `healthScore`, `healthTrend`, `lastAlert`, `openIncidents`, `conditions`.

Optional status sync from the running agent (no Operator yet):

```bash
# CLI
kprompt agent run -n payments --health --analyze --heuristic \
  --agent-cr demo --agent-cr-namespace payments

# Helm
helm upgrade --install kprompt-agent ./charts/kprompt-agent -n payments \
  --set agentCR.name=demo \
  --set agentCR.create=true
```

Then:

```bash
kubectl get kpromptagents -n payments
kubectl get kpa demo -n payments -o yaml   # status.healthScore / status.lastAlert
```

Full Deployment lifecycle for the CR is **AG-014 Operator**.

### Secret keys

| Env key | Purpose |
|---------|---------|
| `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` / … | LLM (same as CLI) |
| `KPROMPT_SLACK_BOT_TOKEN` + `KPROMPT_SLACK_CHANNEL` | Threaded Slack (preferred) |
| `KPROMPT_SLACK_WEBHOOK_URL` | Slack webhook fallback |
| `KPROMPT_WEBHOOK_URL` | Generic AgentAlert JSON POST |
| `KPROMPT_AGENT_CR` (+ `_NAMESPACE`) | Patch KpromptAgent.status |

## Pipeline flags

| Flag | Task |
|------|------|
| `--incidents` | AG-006 correlate |
| `--fetch-logs` | AG-005 on-demand logs |
| `--build-context` | AG-007 context |
| `--analyze` | AG-008 gated AgentAlert |
| `--slack` | AG-009 |
| `--webhook` | AG-010 |
| `--health` | AG-011 score |
| `--agent-cr` | AG-013 status sync |

Autopilot / Operator lifecycle are later tasks (AG-014 · AG-017).
