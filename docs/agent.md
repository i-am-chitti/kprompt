# kprompt Observe agent

In-cluster, **namespace-scoped** agent that continuously watches Pods/Events (and optional workloads), correlates incidents, optionally analyzes with an LLM, and notifies Slack/webhooks.

This is **Observe Mode only** — it never applies, patches, or deletes cluster resources ([ADR-0013](https://github.com/kprompt/kprompt-architecture/blob/main/decisions/ADR-0013-in-cluster-agent.md)). **Autopilot is not shipped** and needs a separate ADR before any mutate path exists.

## Positioning (honest)

| Tool | Job | How kprompt Observe differs |
|------|-----|-----------------------------|
| **K8sGPT** | On-demand / scheduled **analyzer** (scan → explain) | We are **always-on watch → correlated Incident → gated alert**, not a fleet scanner. Use K8sGPT when you want analyzer findings; use this agent when you want threaded Slack alerts from live Events/Pods. |
| **Kagent** | **In-cluster agent framework** (multi-agent CRDs / tools) | We ship one **kprompt-native Observe pipeline** (Incident / AgentAlert + PlanResult DNA), not a general multi-agent platform. Do not expect Kagent feature parity. |
| **kprompt CLI** | Reactive intent compiler (plan → approve → apply) | The agent is **optional**. The laptop CLI still needs no daemon ([ADR-0001](https://github.com/kprompt/kprompt-architecture/blob/main/decisions/ADR-0001-go-cli.md)). |

Explicit non-claims: no silent remediations, no ClusterRole-by-default, no “we host your fleet agent” SaaS, no Autopilot in V1.

## RBAC

Default install is a **Role + RoleBinding in one namespace** (get/list/watch on pods, events, logs, deployments, …). Not a ClusterRole god-mode SA.

- Secrets **watch is off by default**; when enabled (`--watch …,secrets`), only **metadata** is emitted (type + key count) — never Secret values.
- Status sync onto a `KpromptAgent` CR (`--agent-cr`) adds **status patch** verbs for that CR only — still no workload mutate.
- You remain responsible for the ServiceAccount scope you deploy.

## LLM cost

- The agent does **not** call the LLM on every raw API event. It batches by open **Incident**, then applies a **severity + confidence gate** before Slack/webhook.
- Prefer `--heuristic` for demos / offline; turn LLM on when you accept API spend.
- Gate tighter with `--min-severity` / `--min-confidence` (defaults: medium / 0.7) to limit alert fatigue and token burn.
- Credentials stay in a **Secret** (`envFrom`) — never in CRD/ConfigMap plaintext.

## Laptop smoke test

```bash
kprompt agent run -n payments --analyze --fetch-logs --health --heuristic
kprompt agent run -n payments --slack --fetch-logs   # needs Slack env
```

Need a namespace that actually misbehaves? [kprompt-examples](https://github.com/kprompt/kprompt-examples) provisions a kind cluster plus seven failure scenarios (crashloop, image pull, OOM, stalled rollout, unbound PVC, failing CronJob, missing dependency), each documenting what the agent is expected to conclude:

```bash
git clone https://github.com/kprompt/kprompt-examples.git && cd kprompt-examples
make walkthrough   # up → break-all → verify → agent-full (~45s)
```

Or step by step:

```bash
make up
make break SCENARIO=01-crashloop
make verify
kprompt agent run -n payments --analyze --health --heuristic
```

## Helm install (AG-012)

Preferred in-cluster path: [`charts/kprompt-agent`](../charts/kprompt-agent).

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

Chart README: [`charts/kprompt-agent/README.md`](../charts/kprompt-agent/README.md). Website mirror: [kprompt.ai/docs/agent](https://kprompt.ai/docs/agent).

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

## Operator (AG-014)

Optional controller that watches `KpromptAgent` CRs and creates the Observe agent ServiceAccount, Role, RoleBinding, and Deployment.

```bash
# Laptop
kprompt agent operator --once -n payments
kprompt agent operator --in-cluster   # in-cluster via Helm chart

# Helm
kubectl apply -f deploy/crd/kprompt.ai_kpromptagents.yaml
helm upgrade --install kprompt-operator ./charts/kprompt-operator \
  -n kprompt-system --create-namespace \
  --set image.tag=dev \
  --set defaultAgentImage=ghcr.io/kprompt/kprompt:dev
kubectl apply -f config/samples/kpromptagent.yaml
```

Constraints (V1):

- Mode must be **Observe** (Autopilot rejected)
- `spec.namespace` empty or equal to the CR namespace (no cross-namespace)
- Operator uses a **ClusterRole** to manage agent objects; prefer the manual `kprompt-agent` chart if you want Role-only installs

Chart: [`charts/kprompt-operator`](../charts/kprompt-operator).

### Secret keys

| Env key | Purpose |
|---------|---------|
| `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` / … | LLM (same as CLI) |
| `KPROMPT_SLACK_BOT_TOKEN` + `KPROMPT_SLACK_CHANNEL` | Threaded Slack (preferred) |
| `KPROMPT_SLACK_WEBHOOK_URL` | Slack webhook fallback |
| `KPROMPT_WEBHOOK_URL` | Generic AgentAlert JSON POST |
| `KPROMPT_AGENT_CR` (+ `_NAMESPACE`) | Patch KpromptAgent.status |

## Watched resources (AG-004)

Default is `pods,events`. Expand with `--watch`:

```bash
kprompt agent run -n payments \
  --watch pods,events,deployments,replicasets,statefulsets,jobs,cronjobs,pvc,configmaps
```

| Value(s) | Kind |
|----------|------|
| `pods` | Pod |
| `events` | Event |
| `deployments` / `deploy` | Deployment (ready/updated/available) |
| `replicasets` / `rs` | ReplicaSet |
| `statefulsets` / `sts` | StatefulSet |
| `jobs` | Job (Complete/Failed) |
| `cronjobs` / `cj` | CronJob (schedule/suspend) |
| `pvc` | PersistentVolumeClaim (phase) |
| `configmaps` / `cm` | ConfigMap (key count) |
| `secrets` | Secret — **opt-in, metadata only** (never values, ADR-0013) |

Secrets are never watched implicitly and only metadata (type + key count) is emitted.

## Pipeline flags

| Flag | Task |
|------|------|
| `--watch` | AG-004 resource selection |
| `--incidents` | AG-006 correlate |
| `--fetch-logs` | AG-005 on-demand logs |
| `--build-context` | AG-007 context |
| `--analyze` | AG-008 gated AgentAlert |
| `--slack` | AG-009 |
| `--webhook` | AG-010 |
| `--health` | AG-011 score |
| `--agent-cr` | AG-013 status sync |
| `--memory` | AG-015 namespace facts |
| `--patterns` | AG-016 seen-before |
| `--autopilot-propose` | AG-017 / ADR-0015 propose-only |

## Namespace memory (AG-015)

Persists dependency facts (“uses Redis/Kafka/Postgres”) **locally or in-cluster only** — never uploaded to `api.kprompt.ai` by default.

```bash
# Discover + inject into analyzer context while watching
kprompt agent run -n payments --analyze --heuristic --memory

# Manual facts (file backend → ~/.config/kprompt/memory)
kprompt agent memory set -n payments --kind dependency --key redis --value "cache for sessions"
kprompt agent memory discover -n payments
kprompt agent memory list -n payments

# In-cluster ConfigMap backend (Helm agent.memoryBackend=configmap)
kprompt agent memory list -n payments --memory-backend configmap
```

Relevant facts are filtered into `AgentContext.memory` / `namespace_memory:` prompt blocks when the incident text mentions the dependency or infra failure patterns (timeout, connection refused, …).

## Pattern learning (AG-016)

Remembers incident signatures (reason + workload kind + bucket like crashloop/oom) under `~/.config/kprompt/patterns`. When a similar incident appears (≥2 priors), confidence is boosted and root cause is annotated with **Seen before (N×)** — still **Observe-only**; patterns never trigger apply/patch/delete.

```bash
kprompt agent run -n payments --analyze --heuristic --patterns
```

**Not shipped:** silent Autopilot apply. See [ADR-0015](https://github.com/kprompt/kprompt-architecture/blob/main/decisions/ADR-0015-autopilot-mode.md) — MVP is **propose-only**.

## Autopilot (AG-017 · ADR-0015)

Opt-in, allowlist-only. Default remains Observe.

```bash
kprompt agent run -n payments --analyze --heuristic --autopilot-propose
```

- MVP allowlist: `rollbackFailedRollout` only
- Emits `AutopilotProposal` (PlanResult-shaped) + local audit JSONL (`~/.config/kprompt/autopilot`)
- **Never silent apply** in this MVP; `Applied` stays false. Policy/human gate required before any future apply executor
- Hard-deny outside allowlist (same spirit as ADR-0003)
