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

### Secret keys

| Env key | Purpose |
|---------|---------|
| `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` / … | LLM (same as CLI) |
| `KPROMPT_SLACK_BOT_TOKEN` + `KPROMPT_SLACK_CHANNEL` | Threaded Slack (preferred) |
| `KPROMPT_SLACK_WEBHOOK_URL` | Slack webhook fallback |
| `KPROMPT_WEBHOOK_URL` | Generic AgentAlert JSON POST |

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

Autopilot / Operator lifecycle are later tasks (AG-014 · AG-017).
