# Namespace Agent vs Observe vs Coordinator

Honest modes table for operators and contributors ([AG-045](https://github.com/kprompt/kprompt-architecture/issues/203)).

Contracts: [ADR-0013](https://github.com/kprompt/kprompt-architecture/blob/main/decisions/ADR-0013-in-cluster-agent.md) · [ADR-0016](https://github.com/kprompt/kprompt-architecture/blob/main/decisions/ADR-0016-namespace-agent.md) · [ADR-0017](https://github.com/kprompt/kprompt-architecture/blob/main/decisions/ADR-0017-coordinator.md).

## Modes at a glance

| Surface | What it is | Scope | Mutate? | Artifact |
|---------|------------|-------|---------|----------|
| **Observe Mode** | Always-on watch → correlate → gate → notify | One namespace (Role) | **Never** (default) | `Incident` / `AgentAlert` |
| **Namespace Agent** | Observe + multi-signal RCA + memory/patterns + InvestigationReport v2 | One namespace | Propose-only opt-in; apply gated | `InvestigationReport` (schema v2) |
| **Coordinator** | Thin fan-in for **cross-namespace** verification | Cluster handoff API | **Default off** | `CoordinatorHandoff` → `CoordinatorReply` |
| **kprompt CLI** | Laptop intent → plan → approve → apply | kubeconfig context(s) | Only after approval | `PlanResult` |

**Namespace Agent is not a new binary.** It is the Observe agent runtime evolving under ADR-0016 (detectors, confidence, memory-not-proof, GitOps evidence, priority, handoff client). Same Helm chart: [`charts/kprompt-agent`](../charts/kprompt-agent).

## Who does what

```text
┌─────────────────────┐     InvestigationReport v2      ┌──────────────────┐
│  Namespace Agent    │ ── Slack / webhook / ask ──────► │  Humans / SIEM   │
│  (Role, one ns)     │                                  └──────────────────┘
│                     │  Suspect outside my ns?
│                     │ ── CoordinatorHandoff ─────────► ┌──────────────────┐
└─────────────────────┘     (--coordinator-url)          │  Coordinator     │
                                                         │  (thin; no mute  │
                                                         │   by default)    │
                                                         └──────────────────┘
```

| Job | Owner |
|-----|--------|
| Watch Pods/Events/…, correlate incidents | Namespace Agent / Observe |
| Causal RCA, confidence, unknowns | Namespace Agent |
| Slack `status` / `why` / `false positive` | Namespace Agent (`--slack-ask`) |
| Guess another namespace’s root cause | **Nobody** — hand off |
| Route / verify cross-ns suspicion | Coordinator (`kprompt agent coordinator` / Helm chart) |
| Shared Knowledge (durable handoff edges) | `GET /v1/knowledge` · ConfigMap store · `agent coordinator knowledge` |
| Fleet inventory (AG-062) | `kprompt agent list -A` |
| Intelligence brief (AG-065) | `kprompt agent status -n <ns>` |
| Apply / patch / delete workloads | Autopilot **policyAuto** only (AG-042+), never silent |

## Investigation Graph (runtime)

Always-on intelligence is the same **gated graph** as CLI investigate — not a free-form multi-agent fleet.

```text
 Observe / NA (one ns)          Coordinator              Gate
 ─────────────────────          ───────────              ────
 Signal → Detect → RCA ──handoff──► probe / merge ──► notify
      │                                              │
      └── optional Autopilot propose ──► PlanResult ──approve──► apply ──verify
```

| Prefer a **loop** | Prefer a **graph** |
|-------------------|--------------------|
| Single-ns, one incident you are steering | Cross-ns suspect + Coordinator verify |
| Slack ask `why` on one object | Independent signal fan-in before merge |
| Tight hop-by-hop oversight | Width with Role-scoped workers |

**Verify edge:** Coordinator merge requires fresh EvidenceRef (`coordinator-kube-probe`) or honest probe Unknowns — origin analyzer narrative alone caps confidence (AG-068). Full contract: [investigation-graph.md](./investigation-graph.md) ([AG-067](https://github.com/kprompt/kprompt-architecture/issues/213)).

## Non-claims (do not market these)

- Silent or default LLM-said-so remediations
- Cluster-wide god-mode SA on every namespace agent
- “We host your fleet agent” SaaS as the OSS path
- Coordinator that mutates workloads out of the box
- Fabricated Prom / OTel / GitOps values when backends are missing (we **degrade** instead)
- Memory or patterns as sole proof of root cause
- Feature parity with K8sGPT (analyzer) or Kagent (multi-agent framework)
- “1000 agents in one window” / general dynamic-workflow orchestration

## Feature → mode map

| Capability | Flag / surface | Mode |
|------------|----------------|------|
| Watch + incidents + health | `--incidents --health` | Observe |
| LLM / heuristic alert | `--analyze` | Observe → NA |
| Slack / webhook | `--slack` / `--webhook` | Observe |
| Slack ask | `--slack-ask` | NA surface |
| Memory / patterns | `--memory` / `--patterns` | NA learn |
| Prom / OTel / GitOps evidence | env / `--gitops-evidence` | NA signals |
| Priority objectives | automatic (AG-030) | NA reason |
| Coordinator handoff | `--coordinator-url` + `agent coordinator` | NA → Coord |
| Autopilot propose | `--autopilot-propose` | Propose-only |
| Autopilot apply | `--autopilot-apply` / `apply-proposal --approve` | **policyAuto only** |

## Related docs

- Investigation Graph (loop vs graph, verify edges): [investigation-graph.md](./investigation-graph.md)
- Install & pipeline flags: [agent.md](./agent.md)
- Cost / RBAC / ops runbook: [agent-ops.md](./agent-ops.md)
- Demo scenarios: [kprompt-examples](https://github.com/kprompt/kprompt-examples) · [NA loop walkthrough](https://github.com/kprompt/kprompt-examples/blob/main/docs/namespace-agent-loop.md)
- Task backlog: [AGENT-TASKS.md](https://github.com/kprompt/kprompt-architecture/blob/main/AGENT-TASKS.md)
