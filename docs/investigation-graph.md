# Investigation Graph

**Contract:** kprompt treats investigation and remediation as a **gated graph**, not a chat line.

Natural language (or continuous Observe) is the *source*. Typed artifacts (`Investigation`, `InvestigationReport`, `PlanResult`) are the *IR*. Approval is the link-edit. Apply is the binary. Verify closes on evidence.

This doc names the shape ([S-017](https://github.com/kprompt/kprompt-architecture/issues/209) · [AG-067](https://github.com/kprompt/kprompt-architecture/issues/213)). It is **not** a general multi-agent orchestration framework (Kagent lane).

Related: [investigate.md](./investigate.md) · [namespace-agent.md](./namespace-agent.md) · [agent.md](./agent.md) · ADR-0014 · ADR-0016 · ADR-0017 · ADR-0003.

---

## The gated graph

```text
  ┌──────────────┐     ┌──────────────┐     ┌────────────────┐
  │ Signal nodes │     │ Reason nodes │     │ Verify edge    │
  │ Events/logs/ │ ──► │ RCA / find-  │ ──► │ independent    │
  │ metrics/…    │     │ ings / report│     │ evidence/probe │
  └──────────────┘     └──────────────┘     └───────┬────────┘
         ▲                     │                    │
         │                     │ fail / Unknowns    │ pass
         │                     ▼                    ▼
         │              ┌──────────────┐     ┌────────────────┐
         │              │ honest stop  │     │ Merge artifact │
         │              │ or degrade[] │     │ Investigation /│
         │              └──────────────┘     │ Report / Alert │
         │                                   └───────┬────────┘
         │                                           │
         │                              optional suggested fix
         │                                           ▼
         │                                   ┌────────────────┐
         │                                   │ PlanResult IR  │
         │                                   │ + safety/deny  │
         │                                   └───────┬────────┘
         │                                           │
         │                                      approve?
         │                                    yes │    │ no
         │                                        ▼    ▼
         │                                   apply   abort
         │                                        │
         └──────── post-apply verify (T-070) ◄────┘
```

**CLI path (on-demand):** prompt → multi-hop reads → `Investigation` → optional PlanResult → approve → apply → verify.

**Runtime path (always-on):** Observe → Detect/RCA → InvestigationReport → (handoff) → Coordinator probe/verify → merge → notify → optional Autopilot **propose** → approve/apply → verify.

Same DNA: structure before mutate; verify on evidence, not vibes.

---

## Nodes and edges

| Kind | Examples | Rule |
|------|----------|------|
| **Signal node** | Events, logs, Endpoints, Prom/OTel evidence | Real cluster reads; degrade honestly when missing |
| **Reason node** | `investigate` hops, NA detectors, LLM analyze | Produces findings / hypotheses — not proof alone |
| **Verify edge** | Re-read, Coordinator `KubeProbe`, schema/risk stamp, hard deny | **Independent context** — must not reuse the analyzer’s chat narrative as proof ([S-018](https://github.com/kprompt/kprompt-architecture/issues/210) · [AG-068](https://github.com/kprompt/kprompt-architecture/issues/214)) |
| **Merge node** | Investigation assembly, `Coordinator.Merge` | One artifact for humans / Slack / CI |
| **Gate node** | Safety policy, approval, RemediationPolicy | Compilers refuse; chatbots negotiate |
| **Apply / verify** | Executor + post-apply verify | Apply only what was approved; T-070 closes the loop |

An edge exists only when the next step **reads** the previous step’s output. If no data crosses between two boxes, they are independent — candidates for fan-out ([S-019](https://github.com/kprompt/kprompt-architecture/issues/211)), not forced “and then” queues.

---

## Loop vs graph

| Use a **loop** (single agent / sequential hops) when… | Use a **graph** (fan-out + verify + merge) when… |
|--------------------------------------------------------|--------------------------------------------------|
| One object, one bug (`why is this pod CrashLooping?`) | Independent signals or namespaces in parallel |
| Exploratory — you still need to steer | Cross-ns suspicion (NA → Coordinator → probe) |
| Every step truly depends on the last | Multi-signal sweep (Events ∥ metrics ∥ logs) with a merge |
| Tight human oversight of each hop | Width matters and workers stay isolated |

**Tell:** if you cannot find two boxes with *no* arrow between them, there is no graph to build — stay a loop.

---

## Independent verify (non-negotiable)

A second model call in the **same session** is not verification. It is agreement in a different font.

Verify edges must rest on **anchors** the optimizer cannot invent:

- Fresh EvidenceRef / probe reads (`Source: coordinator-kube-probe`)
- Schema + risk stamp + hard denies (policy code)
- Post-apply readiness / goal checks (T-070)
- Honest `Unknowns` / `degraded[]` when evidence is missing

**Coordinator Merge (AG-068):** a suspect report without probe EvidenceRef or honest kube-probe Unknowns is treated as soft-agree — confidence capped at **0.4**, Unknown stamped, no evidence promotion. Fresh probe anchors use the probe confidence as the ceiling.

**Full registry:** [reality-anchors.md](./reality-anchors.md) ([S-020](https://github.com/kprompt/kprompt-architecture/issues/212) · [AG-070](https://github.com/kprompt/kprompt-architecture/issues/216)).

---

## Worker isolation

| Surface | Isolation rule |
|---------|----------------|
| Namespace Agent | **Role-scoped** — never invent foreign-ns root cause |
| Coordinator | Thin fan-in; **mutate default off**; probe is read-only |
| Cross-ns truth | Travels only via `CoordinatorHandoff` / reply merge |
| CLI contexts | Multi-context reads sectioned; mutate stays gated per plan |

Two writers sharing one mutable workspace is an anti-pattern ([AG-069](https://github.com/kprompt/kprompt-architecture/issues/215)). Acceptance checklist + tests live with AG-069; RBAC honesty baseline is [AG-039](https://github.com/kprompt/kprompt-architecture/issues/197). Ops: [agent-ops.md](./agent-ops.md#worker-isolation-ag-069).

---

## Runtime diagram (Observe → Coordinator → Plan)

```text
 payments NA                    Coordinator                 Humans / CI
 ─────────                      ───────────                 ──────────
 Watch / Detect / RCA
       │
 InvestigationReport v2 ──notify──► Slack / webhook
       │
 suspect outside ns?
       │
 CoordinatorHandoff ───────────► route + optional KubeProbe
       │                              │
       │◄──── CoordinatorReply ───────┘
       │      (merged EvidenceRef /
       │       Unknowns — not narrative reuse)
       │
 optional Autopilot propose ──► PlanResult ──approve──► apply ──verify──►
```

CLI on-demand investigate is the same contract without the always-on watch: hops are signal nodes; the `Investigation` JSON is the merge artifact.

---

## When a graph is the wrong choice

Skip fan-out / Coordinator / multi-agent width when:

- The task is small or isolated (one function-equivalent: fix one Pending pod)
- You need tight oversight of every hop before the next runs
- You do not yet know what you are looking for (steer one loop first)
- Steps are genuinely sequential (each hop consumes the prior output)

Forcing a graph onto a true chain only adds coordination cost.

---

## Explicit non-goals

- Claude-style “1000 agents in one window” / free-form dynamic workflow fleets
- Competing with **Kagent** as a general multi-agent framework
- Silent or default LLM-said-so apply
- Treating chat transcript as the IR
- Claiming a second same-session LLM pass as independent verify
- Coordinator default workload mutate
- Uploading raw cluster dumps to `api.kprompt.ai` by default

---

## Shipped vs building

| Piece | Status |
|-------|--------|
| `investigate` / `why` / `timeline` → `Investigation` | Shipped (MVP) |
| PlanResult → approve → apply → post-apply verify | Shipped |
| Observe → Incident / InvestigationReport → notify | Shipped |
| Coordinator handoff + merge + optional probe | Shipped (thin) |
| Named Investigation Graph contract (this doc) | Shipped (docs) |
| Coordinator independent verify edge (AG-068) | Shipped |
| Worker isolation checklist + tests (AG-069) | Shipped |
| Reality anchors registry (S-020 / AG-070) | Shipped |
| Pre-trust independent verify hooks (T-089) | Shipped (`internal/pretrust`) |
| Investigate hop parallelization (T-090) | Shipped |
| Investigate hop parallelization (T-090) | Building |
| Anchors registry (S-020 / AG-070) | Building |

---

## See also

- [investigate.md](./investigate.md) — CLI multi-hop RCA
- [why.md](./why.md) — causal state chain (prefer **loop**)
- [namespace-agent.md](./namespace-agent.md) — modes table
- [simulation.md](./simulation.md) — change preview / blast radius
- Architecture backlog: [SRE-TASKS.md](https://github.com/kprompt/kprompt-architecture/blob/main/SRE-TASKS.md) · [AGENT-TASKS.md](https://github.com/kprompt/kprompt-architecture/blob/main/AGENT-TASKS.md)
