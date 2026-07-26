# Cleanup / unused resources

`cleanup` scans for unused or stale resources (S-007 · T-085):

```bash
kprompt "cleanup payments namespace" -n payments
kprompt "find unused configmaps and secrets" -n production
kprompt "prune my cluster"
kprompt "cleanup" -n shop --output json
```

It reports candidates as an ADR-0014 `Investigation`. The scan itself never
mutates. When stale Jobs / ReplicaSets are found, `cleanup` may offer a single
**reviewable delete plan** (TTY `y/N` or `--approve`). Nothing applies silently,
and `-o json` stays report-only.

## MVP candidates

| Code | What it flags | Follow-up |
|------|----------------|-----------|
| `Cleanup.UnusedConfigMap` | ConfigMap not referenced by any Pod, workload template, or ServiceAccount | Guidance only |
| `Cleanup.UnusedSecret` | Secret not referenced by env/envFrom/volumes/imagePullSecrets/ServiceAccount | Guidance only |
| `Cleanup.CompletedJob` | Job finished more than 24h ago (no `ttlSecondsAfterFinished`) | Approve-gated delete |
| `Cleanup.OldReplicaSet` | Deployment-owned ReplicaSet scaled to zero (superseded revision) | Approve-gated delete |

References scanned include container `env` (`configMapKeyRef` / `secretKeyRef`),
`envFrom`, volumes (`configMap`, `secret`, projected sources), `imagePullSecrets`,
and ServiceAccount `secrets` / `imagePullSecrets`.

```bash
kprompt "cleanup payments" -n payments --output json | jq '.result'
```

## Delete plan (approve-required)

`cleanup` offers **one aggregate delete plan** that only removes named
`Job` and `ReplicaSet` resources already flagged as stale. ConfigMaps and
Secrets are **never** auto-deleted — orphan detection can miss CRD/GitOps
references, so those stay guidance (`describe` / manual delete).

Safety still hard-denies Namespace / wipe-class deletes and unscoped names.

## Honest limits

Service-account tokens, `kube-root-ca.crt`, and docker-config Secrets are
skipped. The MVP does **not** inspect CRD owners, annotations from GitOps
controllers, or cross-namespace references — review candidates before deleting.
A ConfigMap/Secret consumed only by a tool outside the scanned kinds may show
as unused.
