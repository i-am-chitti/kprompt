# Cleanup / unused resources

`cleanup` is a read-only scan for unused or stale resources (S-007 · T-085):

```bash
kprompt "cleanup payments namespace" -n payments
kprompt "find unused configmaps and secrets" -n production
kprompt "prune my cluster"
kprompt "cleanup" -n shop --output json
```

It reports candidates as an ADR-0014 `Investigation`. It **never deletes**;
delete plans with hard-denies and approval are deferred to a later phase.

## MVP candidates

| Code | What it flags |
|------|----------------|
| `Cleanup.UnusedConfigMap` | ConfigMap not referenced by any Pod, workload template, or ServiceAccount |
| `Cleanup.UnusedSecret` | Secret not referenced by env/envFrom/volumes/imagePullSecrets/ServiceAccount |
| `Cleanup.CompletedJob` | Job finished more than 24h ago (no `ttlSecondsAfterFinished`) |
| `Cleanup.OldReplicaSet` | Deployment-owned ReplicaSet scaled to zero (superseded revision) |

References scanned include container `env` (`configMapKeyRef` / `secretKeyRef`),
`envFrom`, volumes (`configMap`, `secret`, projected sources), `imagePullSecrets`,
and ServiceAccount `secrets` / `imagePullSecrets`.

```bash
kprompt "cleanup payments" -n payments --output json | jq '.result'
```

## Honest limits

Service-account tokens, `kube-root-ca.crt`, and docker-config Secrets are
skipped. The MVP does **not** inspect CRD owners, annotations from GitOps
controllers, or cross-namespace references — review candidates before deleting.
A ConfigMap/Secret consumed only by a tool outside the scanned kinds may show
as unused.
