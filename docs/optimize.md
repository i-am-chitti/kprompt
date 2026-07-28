# Optimize my cluster

Read-only capacity and hygiene report (`optimize my cluster`). Never mutates;
optional fix plans still need a separate approval.

```bash
kprompt "optimize my cluster"
kprompt "optimize payments namespace"
kprompt --contexts staging,prod "optimize my cluster"
```

## Sections

| Section | Signal |
|---------|--------|
| Inventory | Deployments / StatefulSets, replicas, requests/limits |
| Idle | Prometheus usage ≪ request (underutilized) |
| Rightsizing | Concrete request/limit deltas from usage |
| HPA | Static-replica / maxed-HPA hints; static Deployments get an optional approve-gated HPA create plan |
| Cost / carbon notes | Optional $/gCO2e estimates on idle + rightsizing **lower** (T-073) |

## Cost / carbon notes (T-073)

When Prometheus-backed idle or rightsizing-lower findings exist **and** inventory
has request quantities, kprompt appends labeled estimates:

- Generic public-cloud list-price averages (not your bill)
- Rough carbon intensity (not region-accurate)
- Missing Prom → section skipped → **no fake costs**
- Missing requests → no estimate for that workload

Look for `costNote` in JSON and the `optimize.cost.notes` rollup finding.

## Safety

Optimize itself is observe-only. Suggested patches/scale still go through the
normal PlanResult approval path (optimize `--approve` does **not** auto-apply).

## Related

- Recipes that chain optimize: [docs/recipes.md](./recipes.md)
- Fleet fan-out: [docs/multi-cluster.md](./multi-cluster.md)
