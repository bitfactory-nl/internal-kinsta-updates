# Phase 3 — Integrations Plan

**Goal:** deployment + plugin-management automation.

Already done from the SPEC Phase 3 checklist: batch ops, cross-project search, Kinsta
vulnerability badges. Remaining work is listed below in build order.

Existing building blocks (do NOT recreate):
- Domain models: `domain.PaidPlugin`, `domain.PluginDiff`, `domain.DiffStatus`,
  `domain.PluginSource`, `domain.SSHTarget` (in `internal/domain/`).
- Config: `config.PluginRepo` (github_token / repo / ref), `config.Notifications`
  (`internal/config/schema.go`).
- Secret resolution: `config.ResolveSecret` (keychain: refs).
- Empty dirs awaiting implementation: `internal/adapters/github`, `internal/adapters/ssh`.

## 1. GitHub paid-plugin repo client (`adapters/github`)
- `client.go` — REST client against `org/wp-paid-plugins`, auth via `PluginRepo.GithubToken`
  (resolved through `config.ResolveSecret`, mirroring the Kinsta client pattern).
- Methods: `GetManifest(ctx)` (reads `manifest.yml`: slug → version → zip path) and
  `DownloadZip(ctx, path)`.
- Manifest unmarshals into `[]domain.PaidPlugin`.
- Test: table-driven against a mocked `httptest` server.

## 2. PluginService (`services/plugin_service.go`)
- Wails-bound, registered in `internal/app/app.go`.
- `ListPaidPlugins()` — manifest from private repo (in-memory cache + `RefreshIndex()`).
- `Diff(projectID, envID)` — compares Kinsta `KinstaPlugin.Version` vs manifest
  `LatestVersion`, returns `[]domain.PluginDiff` with status/vulnerable flags.
- Test: diff logic per status (up_to_date, update_available, not_in_repo, vulnerable).

## 3. SSH adapter (`adapters/ssh/client.go`)
- `golang.org/x/crypto/ssh` (new dependency). Connect using `domain.SSHTarget`.
- `RunCommand(ctx, target, cmd)` for wp-cli, plus session stream for the terminal.
- `PluginService.UpdateViaSSH(projectID, envID, slug)` — download zip → scp/sftp →
  `wp plugin install --force` via wp-cli.
- Security: host key checking, no secrets in logs, context timeouts.

## 4. Embedded SSH terminal
- Frontend: `SshTerminal.tsx` with xterm.js (new frontend dep).
- Backend `SSHService`: `OpenSession/Write/Resize/Close`, output via Wails event
  `ssh:<sessionID>:data`. New tab in `ProjectDetail.tsx`.

## 5. GitHub workflow generator
- `internal/workflows/github_workflow.go` — generates `.github/workflows/check-updates.yml`
  from `.rdm.yml`.
- `KinstaService.GenerateGitHubWorkflow` (preview) + `WriteGitHubWorkflow` (writes file).
- UI button in the existing Updates/Kinsta tab.

## 6. ~~AWS/VPS info panels~~ — DROPPED (2026-06-08)
- Cancelled per user: no AWS/VPS integration wanted. The existing
  `AWSProjectCfg`/`VPSProjectCfg` domain types are left in place but no panels
  are built. The SSH adapter (step 3) remains, used for Kinsta plugin updates.

## 7. Vulnerability notifications
- Background scan loop (interval from `Notifications.ScanIntervalMinutes`), checks each
  project's Kinsta plugins for `IsVersionVulnerable`, calls `NotifyService.Send`.
  Gated by `EnableVulnerabilityAlerts`.

## Out of scope / separate decision
- App auto-update channel (checklist item) — unrelated to plugin management; plan separately.
- AWS/VPS info panels (was step 6) — dropped, see above.

## Recommended order
1 → 2 (value without SSH: see what's outdated) → 3 (perform updates) → 4/5/7.

---

## Status
- [x] Step 1 — GitHub paid-plugin repo client (`internal/adapters/github`)
- [x] Step 2 — PluginService (`internal/services/plugin_service.go`, wired in app.go)
- [x] Step 3 — SSH adapter (`internal/adapters/ssh`) + `PluginService.UpdateViaSSH`
- [x] Step 4 — Embedded SSH terminal (`SSHService` + adapter `OpenShell`/`Session`, `SshTerminalTab.tsx` with xterm.js)
- [ ] Step 5 — GitHub workflow generator
- [~] Step 6 — AWS/VPS info panels — DROPPED (no AWS/VPS integration wanted)
- [x] Step 7 — Vulnerability notifications (`internal/services/vuln_scan_service.go`, background loop + Scan())
