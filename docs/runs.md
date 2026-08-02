# App runs & CLI bridge

Team `/run` lets you compose a prompt in the browser
([app.kprompt.ai/run](https://app.kprompt.ai/run)). Execution always happens on
an enrolled laptop with local kubeconfig — never inside `api.kprompt.ai`.

If nobody is running `kprompt run listen`, the job stays **queued**. That is
intentional ([ADR-0021](https://github.com/kprompt/kprompt-architecture/blob/main/decisions/ADR-0021-app-initiated-runs.md)),
not a hang.

Website mirror: [kprompt.ai/docs/runs](https://kprompt.ai/docs/runs).

## Flow

```text
Browser (compose)  →  api.kprompt.ai  (status: queued)
                              ↑ claim
kprompt run listen →  local plan pipeline (kubeconfig stays local)
                   →  PlanResult / awaiting_approve
```

Hard rules:

- No kubeconfigs or cluster tokens in the control plane
- Same local safety + cached org policy as normal CLI plans
- Mutations never auto-apply from the plane

## 1. Enroll (`kprompt login`)

```bash
kprompt login            # user code → approve at app.kprompt.ai/connect
kprompt login --open
kprompt whoami
```

1. CLI prints a user code and Connect URL.
2. Sign in to the Team app if needed.
3. Open **Connect CLI**, approve the code.
4. Token lands in `~/.kprompt/credentials.yaml` (0600).

See also [Team enrollment](https://kprompt.ai/docs/team).

## 2. Start the bridge

```bash
kprompt run listen
# kprompt run listen --interval 3s --worker-label laptop-dev
```

Leave it running while you use `/run`. The worker polls `POST /v1/runs/claim`,
runs the plan pipeline (**never auto-applies**), and posts
`POST /v1/runs/{id}/result`.

## 3. Queue a prompt in the app

1. Open [app.kprompt.ai/run](https://app.kprompt.ai/run).
2. Enter prompt, optional namespace / context hint.
3. Pick `approve_mode` (`plan_only` | `require_approve` | `auto_if_policy_allows`).
4. Queue — status starts as **queued** until a bridge claims it.

## Why status stays `queued`

| Symptom | Likely cause | Fix |
|---------|--------------|-----|
| Stuck on queued | No `run listen` | Start the bridge; keep the terminal open |
| Claim fails | Stale / missing `kp_…` | `kprompt logout && kprompt login` |
| Wrong cluster | `context_hint` mismatch | Fix hint or `kprompt config alias set …` |
| Doctor Team FAIL | Forbidden / expired session | Re-login; confirm org membership |

## Approve modes

| Mode | Bridge behavior |
|------|-----------------|
| `plan_only` | Plan + post PlanResult; never apply (including Replay / drill) |
| `require_approve` | Mutating plans pause at `awaiting_approve` until Approve in the app |
| `auto_if_policy_allows` | May apply only when org policy allows; hard denies still block |

## Replay / drill

Audit / run detail **Queue drill run** re-queues the same prompt as `plan_only`
with a staging-ish context hint (prod-like hints blocked). Still needs a live
`run listen` worker.

**Gotcha:** drill defaults `context_hint` to something like `staging`. If that
name is not a local kubeconfig context or alias, the bridge fails after claim.
Map it first (`kprompt config alias set staging kind-kprompt-demo`) or queue a
fresh run with an empty / real context hint.

## Why status is `failed` (after claim)

`queued` → `running` → **`failed`** means the laptop claimed the job but the
local plan pipeline errored before a PlanResult existed. Check:

1. **Run detail in the app** — red `error` field (authoritative).
2. **Bridge terminal** — recent builds log `Posted run_… → failed: <message>`.
3. **`kprompt doctor`** on the same machine as `run listen`.

| Error (typical) | Cause | Fix |
|-----------------|-------|-----|
| missing API key / provider | No BYOK key for configured provider | `export KPROMPT_GEMINI_API_KEY=…` (or Ollama) · or `kprompt secrets pull` |
| kube context `"staging"` not found | Drill / compose hint ≠ local contexts | Empty hint, real context name, or `config alias set` |
| 429 / quota exceeded | Gemini (or other) free-tier limit | Wait for reset, switch model, or use Ollama — [Providers](https://kprompt.ai/docs/providers) |
| Org policy max_risk … exceeds | Plan risk above cached org ceiling | Soften org `max_risk`, or use a read-only / lower-risk prompt |
| unknown intent | Prompt outside supported ops (e.g. “create a cluster”) | Rephrase to a supported intent — [Commands](https://kprompt.ai/docs/commands) |

Local sanity check (same kube + provider as the bridge):

```bash
kprompt doctor
kprompt --context kind-kprompt-demo "list pods" -n default
# then re-queue in the app and keep: kprompt run listen
```

## Not this

- Not a hosted cluster browser
- Not the in-cluster Observe agent ([docs/agent.md](./agent.md))
- Not silent Autopilot apply

## Related

- CLI: `kprompt run listen --help`
- [docs/providers.md](./providers.md) · [docs/safety.md](./safety.md) · README Team enrollment section
- Website: [kprompt.ai/docs/runs](https://kprompt.ai/docs/runs)
