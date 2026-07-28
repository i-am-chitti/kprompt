# Native HPA create

Create an `autoscaling/v2` HorizontalPodAutoscaler for a Deployment — reviewable plan, approval-gated apply.

```bash
kprompt "add HPA for redis"
kprompt "create HPA for api min 2 max 8 cpu 60" -n prod
kprompt "autoscale payments with cpu" --approve
```

## Defaults

| Field | Default |
|---|---|
| Object name | `{deployment}-hpa` |
| `minReplicas` | `1` (native HPA cannot scale to 0) |
| `maxReplicas` | `10` |
| CPU target | `70%` average utilization |
| Memory target | omitted unless requested |

## Disambiguation

| Prompt | Kind |
|---|---|
| `add HPA for redis` / `autoscale api with cpu` | `hpa` |
| `scale api to 3` | `scale` |
| KEDA / ScaledObject / queue / scale-to-zero | `keda` |
| `optimize my cluster` (HPA hints) | `optimize` |

Scale-to-zero and event-driven autoscaling stay on the [KEDA](../README.md) path.
