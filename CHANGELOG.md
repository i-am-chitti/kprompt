# Changelog

All notable changes to kprompt are documented here. Versions follow [GitHub Releases](https://github.com/kprompt/kprompt/releases).

## [v0.7.0](https://github.com/kprompt/kprompt/releases/tag/v0.7.0) — 2026-07-28

Community-powered patch-plus release: first-wave contributor PRs, plus day-2 CLI features shipped on `main` since v0.6.0.

### Thanks

Huge thanks to first-time and returning contributors:

- [@syloe1](https://github.com/syloe1) — docs, tests, charts, UX copy (#19, #27–#32)
- [@atharvafulay](https://github.com/atharvafulay) — doctor Helm setup hint (#43)
- [@terminalchai](https://github.com/terminalchai) — detect helper unit tests (#26)
- [@pollychen-lab](https://github.com/pollychen-lab) — contexts help clarification (#9)

### Community contributions

- **Docs / help:** theme flag docs; Cobra `Example` fields for `doctor` / `contexts` / `history`; GitOps PR flags in README; `learn --show` header clarity; contexts help text
- **Tests:** UI helpers, route helpers, tools detect helpers
- **Charts:** `kprompt-coordinator` Helm `NOTES.txt`
- **UX:** doctor Helm-missing hint points at `kprompt setup` (no longer “coming soon”)

### Features (since v0.6.0)

- **`kprompt setup`** — detect/plan + approve-gated host Helm and cluster operator installs (T-062…T-064)
- **GitOps PR mode** — `--gitops` opens/updates a GitHub PR instead of applying live (T-072)
- **`kprompt learn`** — local cluster tool profiles (S-009)
- **Drift scan** — GitOps out-of-sync apps (S-008)
- **Recipes** — curated packs that expand to approve-gated routes (S-013)
- **Optimize** — labeled cost/carbon estimate notes on idle/rightsizing (T-073)
- **Moonshot / Kimi K3** — named BYOK provider preset

### Notes

Experimental — prefer non-production clusters. Autopilot remains propose-only by default.

## [v0.6.0](https://github.com/kprompt/kprompt/releases/tag/v0.6.0) — 2026-07-26

Namespace Agent pack: multi-signal Observe (Prom/OTel/GitOps), InvestigationReport v2, Slack ask, thin Coordinator, gated Autopilot (`policyAuto`), plus CLI investigate/why/timeline/impact/audit/cleanup.
