# Audit / security hygiene

`audit` is a read-only security and hygiene scan (S-006 · T-084):

```bash
kprompt "audit payments namespace" -n payments
kprompt "security scan" -n production
kprompt "audit my cluster"
kprompt "hygiene check" -n shop --output json
```

It walks Deployment, StatefulSet, and DaemonSet pod templates and emits an
ADR-0014 `Investigation` with coded findings. It never mutates and never asks
for approval.

## MVP checks

| Code | What it flags |
|------|----------------|
| `Audit.RunAsRoot` | `runAsNonRoot` is not `true` on container or pod |
| `Audit.Privileged` | `securityContext.privileged=true` |
| `Audit.PrivilegeEscalation` | `allowPrivilegeEscalation=true` (explicit) |
| `Audit.LatestTag` | image is untagged or tagged `latest` (digests OK) |
| `Audit.MissingImagePullPolicy` | empty `imagePullPolicy` with a mutable tag |
| `Audit.MissingRequests` | missing CPU or memory requests |
| `Audit.MissingLimits` | missing CPU or memory limits |
| `Audit.HostNamespace` | `hostNetwork` / `hostPID` / `hostIPC` |

```bash
kprompt "audit payments" -n payments --output json | jq '.result'
```

## Honest limits

This MVP does **not** cover NetworkPolicy gaps, RBAC over-permission,
PodSecurity admission levels, or `readOnlyRootFilesystem`. Findings are
static template rules — not a CIS benchmark or live runtime attestation.

Suggested harden patches (approve-required) are deferred; review findings
manually before changing production workloads.
