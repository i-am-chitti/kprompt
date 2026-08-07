# Learn loop (RT-001…)

kprompt’s AI Runtime Learn path closes Observe → Plan → Approve → Execute with **durable outcomes** that reshape the next “seen before” boost.

## Signals

| Signal | Outcome | Effect on pattern store |
|--------|---------|-------------------------|
| Alert recovered | `resolved` | `Confirmed++`, weight +0.05 |
| Slack `false positive` | `false_positive` | `FalsePositives++`, weight −0.15 (floor 0.2) |
| Autopilot apply + verify `ok` | `apply_success` | same as resolved (+ `LastActionID`) |
| Autopilot apply error or verify `failed` | `apply_failed` | weight −0.10 (**not** FP) |
| Verify `pending` | `apply_partial` | weight −0.02 |

**RT-002 ranking:** when multiple Autopilot actions match, prefer `LastActionID` with healthy weight; dampen when FP-heavy / low weight. `ActionConfidence` and `learnNote` reflect bias; MinConfidence gate still uses raw alert confidence (evidence-not-proof).

## Surfaces

- `kprompt agent run … --patterns --autopilot-propose [--autopilot-apply]`
- `kprompt agent autopilot apply-proposal --approve --patterns …`
- `kprompt agent proposals list|show|apply` — durable store (RT-007)
- Incident fields: `lastApplyOutcome`, `lastVerifyStatus`, `lastActionId`

## Non-goals

- Silent fleet heal
- Treating Learn weight as approval to skip Safety / RemediationPolicy
