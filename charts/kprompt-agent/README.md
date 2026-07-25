# kprompt-agent Helm chart

Namespace-scoped **Observe Mode** agent for [kprompt](https://github.com/kprompt/kprompt).

Deploys a single Deployment that runs:

```text
kprompt agent run --namespace <ns> --in-cluster [--analyze] [--fetch-logs] [--health] …
```

Never mutates the cluster (ADR-0013). **Observe Mode only** — Autopilot is not shipped.

Honest positioning (vs K8sGPT / Kagent, RBAC, LLM cost): [docs/agent.md](../../docs/agent.md) · [kprompt.ai/docs/agent](https://kprompt.ai/docs/agent).

## Install

```bash
# Build/push image (until published tags exist)
docker build -t ghcr.io/kprompt/kprompt:dev .
docker push ghcr.io/kprompt/kprompt:dev

# Secret with provider + notifier credentials (do not commit values)
kubectl -n payments create secret generic kprompt-agent \
  --from-literal=OPENAI_API_KEY=sk-... \
  --from-literal=KPROMPT_SLACK_BOT_TOKEN=xoxb-... \
  --from-literal=KPROMPT_SLACK_CHANNEL=C...

helm upgrade --install kprompt-agent ./charts/kprompt-agent \
  -n payments --create-namespace \
  --set image.tag=dev \
  --set agent.slack=true
```

## Values (high level)

| Key | Default | Notes |
|-----|---------|-------|
| `watchNamespace` | release ns | Namespace the agent watches |
| `image.repository` | `ghcr.io/kprompt/kprompt` | Container image |
| `agent.analyze` | `true` | AG-008 analyzer |
| `agent.fetchLogs` | `true` | AG-005 on-demand logs |
| `agent.health` | `true` | AG-011 health score |
| `agent.slack` / `agent.webhook` | `false` | Enable notifiers |
| `secret.name` | `kprompt-agent` | Env-from Secret |
| `rbac.create` | `true` | Namespace Role (get/list/watch) |
| `agentCR.name` | `""` | Patch `KpromptAgent.status` (AG-013) |
| `agentCR.create` | `false` | Also create the CR from values |

The chart installs the `KpromptAgent` CRD from [`crds/`](./crds). Sample: [`config/samples/kpromptagent.yaml`](../../config/samples/kpromptagent.yaml).

See [values.yaml](./values.yaml) and [docs/agent.md](../../docs/agent.md).
