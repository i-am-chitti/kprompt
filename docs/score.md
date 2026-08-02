# Score / cluster scorecard

`score` rolls reliability, security, and cost into one read-only scorecard (S-011):

```bash
kprompt "score payments namespace" -n payments
kprompt "scorecard for the cluster"
kprompt "health score" -n shop --output json
```

It reuses **audit** (security hygiene) and **optimize** inventory / idle /
rightsizing / HPA signals. Optional Prometheus improves the cost dimension.

## Dimensions

| Dimension | Source | Notes |
|-----------|--------|-------|
| `security` | `audit` findings | Severity-weighted deductions with evidence codes |
| `reliability` | inventory readiness + missing requests/limits + HPA hints | Always scored from API objects |
| `cost` | idle + rightsizing-lower (+ cost notes) | **Skipped** when Prometheus is missing — no fake $/precision |

Overall is the average of **available** dimensions. When cost is skipped, verdict
stays honest (`good`/`fair`/… without claiming a full three-axis score).

```bash
kprompt "score payments namespace" -n payments --output json | jq '.result'
```

JSON `result` is a `Scorecard` with `overall`, `verdict`, `dimensions[]`, and
per-dimension `evidence[]` (`source`, `code`, `impact`, resource links).

## Honest limits

- Not a CIS benchmark, Kubecost bill, or SLA product.
- Without Prometheus, **cost is omitted** (listed under `degraded`) — never invented.
- Security scores only MVP audit rules ([audit.md](./audit.md)).
- Playful “how's my cluster” stays **roast**, not score.

`score` never mutates and never asks for approval.

See also: [Audit](./audit.md) · [Optimize](./optimize.md).
