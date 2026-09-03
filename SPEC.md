# RDM Sites Tool — Technical Specification

**Version:** 1.0
**Status:** Draft
**Target Platform:** macOS (Apple Silicon + Intel)
**Last Updated:** 2026-04-30

---

## 1. Project Overview & Goals

### 1.1 Purpose

RDM Sites Tool is a macOS-native desktop application for a web agency team of 6–7 people. It provides a unified visual overview of all client web projects stored in local folders, combining:

- **Git management** equivalent to SourceTree (history, diffs, staging, branching, remotes).
- **Deployment/hosting status** for Kinsta, AWS, and VPS-hosted sites.
- **WordPress plugin management**, including paid plugin distribution and vulnerability monitoring.

### 1.2 Goals

1. **Single pane of glass** — every project's Git status, host environment, and plugin health visible at once.
2. **Fast** — the sidebar should render Git status for 50+ repos in under 1 second using `go-git`.
3. **Familiar** — SourceTree-equivalent Git UX so designers and developers can switch without retraining.
4. **Native** — feels like a Mac app: notarized, signed, Keychain-integrated, native notifications.
5. **Team-shareable config** — per-project `.rdm/config.yml` checked into the repo so any teammate sees the same metadata.

### 1.3 Non-Goals

- Cross-platform support (Windows / Linux) — macOS only for v1.
- Replacing IDEs — quick-open delegates to Cursor / VSCode / PHPStorm.
- Replacing `kinsta-cli` for advanced server management.
- Hosting WordPress plugin distribution as a service — the private GitHub repo is the source of truth.

### 1.4 Target Users

Internal agency team of 6–7 (developers, designers, project leads). Distribution is internal (signed `.dmg` shared via S3/Dropbox, or via a small auto-update endpoint).

---

## 2. Architecture Diagram

```
+------------------------------------------------------------------+
|                       macOS Desktop App                           |
|                                                                   |
|  +-----------------------------+    +-------------------------+   |
|  |     Frontend (WebView)      |    |     Go Backend          |   |
|  |  React 18 + TS + Tailwind   |<-->|     Wails v3 Bindings   |   |
|  |  shadcn/ui + Zustand        |    |                         |   |
|  |                             |    |  +-------------------+  |   |
|  |  - ProjectList Sidebar      |    |  |   Services Layer  |  |   |
|  |  - GitGraph / DiffViewer    |    |  |  - ProjectService |  |   |
|  |  - KinstaPanel              |    |  |  - GitService     |  |   |
|  |  - PluginUpdates Table      |    |  |  - KinstaService  |  |   |
|  |  - Embedded Terminal (xterm)|    |  |  - PluginService  |  |   |
|  +-----------------------------+    |  |  - SSHService     |  |   |
|                                     |  |  - ConfigService  |  |   |
|                                     |  +-------------------+  |   |
|                                     |          |              |   |
|                                     |          v              |   |
|                                     |  +-------------------+  |   |
|                                     |  |   Adapters        |  |   |
|                                     |  |  - go-git (read)  |  |   |
|                                     |  |  - git CLI (write)|  |   |
|                                     |  |  - Kinsta REST    |  |   |
|                                     |  |  - GitHub REST    |  |   |
|                                     |  |  - SSH (x/crypto) |  |   |
|                                     |  +-------------------+  |   |
|                                     +-------------------------+   |
|                                                |                  |
+------------------------------------------------|------------------+
                                                 |
            +-----------+-------------+----------+----------+--------------+
            |           |             |                     |              |
            v           v             v                     v              v
      +---------+ +-----------+ +---------+         +-------------+ +-------------+
      | Local   | | Kinsta    | | GitHub  |         | macOS       | | macOS       |
      | Git     | | REST API  | | REST    |         | Keychain    | | Notification|
      | Repos   | | v2        | | API     |         | (secrets)   | | Center      |
      +---------+ +-----------+ +---------+         +-------------+ +-------------+
                                     |
                                     v
                          +----------------------+
                          | Private plugin repo  |
                          | (paid plugin zips)   |
                          +----------------------+
```

### 2.1 Layer Responsibilities

| Layer | Responsibility |
|---|---|
| Frontend | Rendering, user interaction, local UI state. No business logic. |
| Wails bindings | Thin marshaling layer between JS and Go. |
| Services | Use-case orchestration. Each service is a Go struct with methods bound to Wails. |
| Adapters | Pure I/O wrappers around external systems. Mockable interfaces. |

---

## 3. Directory Structure

```
RDM-Sites-tool/
├── SPEC.md
├── README.md
├── go.mod
├── go.sum
├── wails.json                  # Wails v3 config
├── main.go                     # Wails app entry point
│
├── internal/
│   ├── app/
│   │   ├── app.go              # App lifecycle, service wiring
│   │   └── menu.go             # Native macOS menu bar
│   │
│   ├── config/
│   │   ├── global.go           # ~/.config/rdm/config.yml loader
│   │   ├── project.go          # .rdm/config.yml loader/validator
│   │   ├── schema.go           # Go structs for both configs
│   │   └── keychain.go         # macOS Keychain wrapper (security cmd / go-keyring)
│   │
│   ├── services/
│   │   ├── project_service.go  # discover, list, watch project folders
│   │   ├── git_service.go      # all git operations
│   │   ├── kinsta_service.go   # Kinsta orchestration
│   │   ├── plugin_service.go   # plugin diff (private repo vs Kinsta)
│   │   ├── ssh_service.go      # embedded terminal + remote command runner
│   │   ├── search_service.go   # cross-project / cross-branch search
│   │   ├── notify_service.go   # macOS notifications
│   │   └── editor_service.go   # quick open in Cursor/VSCode/PHPStorm
│   │
│   ├── adapters/
│   │   ├── gitread/            # go-git wrappers (status, log, diff, blame)
│   │   │   ├── reader.go
│   │   │   ├── graph.go        # commit graph builder
│   │   │   └── blame.go
│   │   ├── gitcli/             # os/exec wrappers (push, pull, rebase, ...)
│   │   │   ├── runner.go       # exec.CommandContext, env, cwd
│   │   │   └── commands.go     # Push, Pull, Fetch, Rebase, CherryPick, ...
│   │   ├── kinsta/             # Kinsta REST client
│   │   │   ├── client.go
│   │   │   ├── sites.go
│   │   │   ├── environments.go
│   │   │   ├── plugins.go
│   │   │   ├── themes.go
│   │   │   └── operations.go
│   │   ├── github/             # GitHub REST client (paid plugin repo)
│   │   │   ├── client.go
│   │   │   └── releases.go
│   │   └── ssh/
│   │       └── client.go       # golang.org/x/crypto/ssh
│   │
│   ├── domain/                 # pure data models, no I/O
│   │   ├── project.go
│   │   ├── git.go
│   │   ├── kinsta.go
│   │   ├── plugin.go
│   │   └── errors.go
│   │
│   └── workflows/
│       └── github_workflow.go  # generate .github/workflows/check-updates.yml
│
├── frontend/
│   ├── package.json
│   ├── vite.config.ts
│   ├── tsconfig.json
│   ├── tailwind.config.ts
│   ├── index.html
│   └── src/
│       ├── main.tsx
│       ├── App.tsx
│       ├── lib/
│       │   ├── wails.ts        # typed wrappers around generated bindings
│       │   ├── store.ts        # Zustand stores
│       │   └── utils.ts
│       ├── hooks/
│       │   ├── useProjects.ts
│       │   ├── useGitStatus.ts
│       │   └── useKinsta.ts
│       ├── components/
│       │   ├── layout/
│       │   │   ├── Shell.tsx
│       │   │   ├── Sidebar.tsx
│       │   │   └── StatusBar.tsx
│       │   ├── projects/
│       │   │   ├── ProjectList.tsx
│       │   │   ├── ProjectRow.tsx
│       │   │   └── ProjectDetail.tsx
│       │   ├── git/
│       │   │   ├── CommitGraph.tsx
│       │   │   ├── CommitList.tsx
│       │   │   ├── DiffViewer.tsx
│       │   │   ├── StagingArea.tsx
│       │   │   ├── BranchList.tsx
│       │   │   ├── StashPanel.tsx
│       │   │   ├── TagsPanel.tsx
│       │   │   ├── BlameView.tsx
│       │   │   └── FileHistory.tsx
│       │   ├── kinsta/
│       │   │   ├── EnvironmentCard.tsx
│       │   │   ├── PluginUpdatesTable.tsx
│       │   │   └── ThemeUpdatesTable.tsx
│       │   ├── deploy/
│       │   │   ├── AwsInfo.tsx
│       │   │   └── VpsInfo.tsx
│       │   ├── terminal/
│       │   │   └── SshTerminal.tsx        # xterm.js
│       │   ├── search/
│       │   │   └── GlobalSearch.tsx
│       │   └── ui/                        # shadcn/ui primitives
│       └── styles/
│           └── globals.css
│
├── build/
│   ├── darwin/
│   │   ├── Info.plist
│   │   ├── entitlements.plist
│   │   └── icon.icns
│   └── scripts/
│       ├── notarize.sh
│       └── sign.sh
│
├── docs/
│   ├── adr/                    # Architecture Decision Records
│   └── images/
│
└── tests/
    ├── integration/
    └── fixtures/
```

---

## 4. Feature Breakdown by Phase

### Phase 1 — MVP (Weeks 1–3)

Goal: replace the most painful daily workflows. Read-only Git + Kinsta visibility.

- [ ] App shell (Wails v3 + React + Tailwind + shadcn/ui boots).
- [ ] Project discovery: scan a configurable root folder (e.g. `~/Projects`) for repos containing `.rdm/config.yml`.
- [ ] Project list sidebar with display name, provider badge, branch, ahead/behind, dirty marker.
- [ ] Git read-only views: status, current branch, last 100 commits, working tree diff, staged diff.
- [ ] Global config + per-project config loader/validator.
- [ ] Kinsta client: list sites, list environments, list plugins per environment.
- [ ] Plugin updates table per project (data only, no actions).
- [ ] Quick open in editor (Cursor / VSCode / PHPStorm).
- [ ] Keychain integration for secrets.
- [ ] Code signing + local notarization pipeline working.

### Phase 2 — Full Git (Weeks 4–6)

Goal: SourceTree replacement.

- [ ] Visual commit graph (multi-branch).
- [ ] Staging: stage/unstage files and hunks.
- [ ] Commit (with message editor, sign-off, amend).
- [ ] Push / Fetch / Pull (via git CLI).
- [ ] Branches: create, checkout, delete, merge.
- [ ] Remote branches: view, checkout tracking, delete remote.
- [ ] Stash: create, apply, pop, drop.
- [ ] Tags: create (lightweight + annotated), push, delete.
- [ ] Cherry-pick.
- [ ] Rebase (onto branch, interactive launches `$EDITOR` in a terminal pane).
- [ ] Submodules: list, init, update.
- [ ] Blame view (file-level, line-level).
- [ ] File history.
- [ ] Conflict resolution helper (open in editor, mark resolved).

### Phase 3 — Integrations (Weeks 7–10)

Goal: deployment + plugin management automation.

- [ ] Batch operations: pull-all, fetch-all, status-across-all.
- [ ] Cross-project / cross-branch search.
- [ ] Kinsta update dashboard with vulnerability flags (red badges).
- [ ] Auto-generate `.github/workflows/check-updates.yml` from `.rdm/config.yml`.
- [ ] AWS/VPS info panels (read-only display from `.rdm/config.yml`).
- [ ] Embedded SSH terminal (xterm.js + `golang.org/x/crypto/ssh`).
- [ ] macOS native notifications for vulnerable plugin updates.
- [ ] Paid plugin sync: compare private repo zips vs Kinsta installed versions; trigger update via SSH.
- [ ] Auto-update channel for the app itself.

---

## 5. Data Models (Go)

All domain types live in `internal/domain/` and are JSON-marshaled across the Wails boundary.

### 5.1 Project

```go
package domain

import "time"

type Provider string

const (
    ProviderKinsta Provider = "kinsta"
    ProviderAWS    Provider = "aws"
    ProviderVPS    Provider = "vps"
    ProviderNone   Provider = "none"
)

type Project struct {
    ID          string        `json:"id"`          // hash of absolute path
    Path        string        `json:"path"`        // absolute filesystem path
    DisplayName string        `json:"displayName"`
    Provider    Provider      `json:"provider"`
    Config      ProjectConfig `json:"config"`
    Git         GitStatus     `json:"git"`
    LastScanAt  time.Time     `json:"lastScanAt"`
}

type ProjectConfig struct {
    Provider    Provider          `yaml:"provider"     json:"provider"`
    DisplayName string            `yaml:"display_name" json:"displayName"`
    Kinsta      *KinstaProjectCfg `yaml:"kinsta,omitempty" json:"kinsta,omitempty"`
    AWS         *AWSProjectCfg    `yaml:"aws,omitempty"    json:"aws,omitempty"`
    VPS         *VPSProjectCfg    `yaml:"vps,omitempty"    json:"vps,omitempty"`
    SSH         *SSHTarget        `yaml:"ssh,omitempty"    json:"ssh,omitempty"`
}
```

### 5.2 Git

```go
package domain

import "time"

type GitStatus struct {
    Branch        string       `json:"branch"`
    Upstream      string       `json:"upstream"`
    Ahead         int          `json:"ahead"`
    Behind        int          `json:"behind"`
    Staged        []FileChange `json:"staged"`
    Unstaged      []FileChange `json:"unstaged"`
    Untracked     []string     `json:"untracked"`
    Conflicted    []string     `json:"conflicted"`
    HasSubmodules bool         `json:"hasSubmodules"`
}

type ChangeKind string

const (
    ChangeAdded    ChangeKind = "added"
    ChangeModified ChangeKind = "modified"
    ChangeDeleted  ChangeKind = "deleted"
    ChangeRenamed  ChangeKind = "renamed"
)

type FileChange struct {
    Path       string     `json:"path"`
    OldPath    string     `json:"oldPath,omitempty"`
    Kind       ChangeKind `json:"kind"`
    Insertions int        `json:"insertions"`
    Deletions  int        `json:"deletions"`
}

type Commit struct {
    Hash       string    `json:"hash"`
    ShortHash  string    `json:"shortHash"`
    Author     Person    `json:"author"`
    Committer  Person    `json:"committer"`
    Message    string    `json:"message"`
    Subject    string    `json:"subject"`
    Parents    []string  `json:"parents"`
    Refs       []string  `json:"refs"`
    AuthoredAt time.Time `json:"authoredAt"`
}

type Person struct {
    Name  string `json:"name"`
    Email string `json:"email"`
}

type Branch struct {
    Name       string `json:"name"`
    FullRef    string `json:"fullRef"`
    IsRemote   bool   `json:"isRemote"`
    IsCurrent  bool   `json:"isCurrent"`
    Upstream   string `json:"upstream,omitempty"`
    HeadCommit string `json:"headCommit"`
}

type Tag struct {
    Name      string    `json:"name"`
    Annotated bool      `json:"annotated"`
    Message   string    `json:"message,omitempty"`
    Commit    string    `json:"commit"`
    TaggedAt  time.Time `json:"taggedAt"`
}

type Stash struct {
    Index     int       `json:"index"`
    Message   string    `json:"message"`
    Branch    string    `json:"branch"`
    StashedAt time.Time `json:"stashedAt"`
}

type FileDiff struct {
    Path    string     `json:"path"`
    OldPath string     `json:"oldPath,omitempty"`
    Binary  bool       `json:"binary"`
    Hunks   []DiffHunk `json:"hunks"`
}

type DiffHunk struct {
    Header   string     `json:"header"`
    OldStart int        `json:"oldStart"`
    OldLines int        `json:"oldLines"`
    NewStart int        `json:"newStart"`
    NewLines int        `json:"newLines"`
    Lines    []DiffLine `json:"lines"`
}

type DiffLineKind string

const (
    LineContext DiffLineKind = "context"
    LineAdd     DiffLineKind = "add"
    LineDel     DiffLineKind = "del"
)

type DiffLine struct {
    Kind    DiffLineKind `json:"kind"`
    Content string       `json:"content"`
    OldNum  int          `json:"oldNum,omitempty"`
    NewNum  int          `json:"newNum,omitempty"`
}

type BlameLine struct {
    LineNo  int       `json:"lineNo"`
    Commit  string    `json:"commit"`
    Author  Person    `json:"author"`
    Date    time.Time `json:"date"`
    Content string    `json:"content"`
}
```

### 5.3 Kinsta

```go
package domain

import "time"

type KinstaProjectCfg struct {
    SiteID       string                      `yaml:"site_id"      json:"siteId"`
    Environments map[string]KinstaEnvBinding `yaml:"environments" json:"environments"`
}

type KinstaEnvBinding struct {
    EnvID  string `yaml:"env_id" json:"envId"`
    Branch string `yaml:"branch" json:"branch"`
}

type KinstaSite struct {
    ID           string             `json:"id"`
    Name         string             `json:"name"`
    DisplayName  string             `json:"displayName"`
    CompanyID    string             `json:"companyId"`
    Environments []KinstaEnvironment `json:"environments"`
}

type KinstaEnvironment struct {
    ID          string    `json:"id"`
    Name        string    `json:"name"`
    DisplayName string    `json:"displayName"`
    IsPremium   bool      `json:"isPremium"`
    Domain      string    `json:"domain"`
    SSH         SSHTarget `json:"ssh"`
}

type SSHTarget struct {
    Host string `json:"host" yaml:"host"`
    Port int    `json:"port" yaml:"port"`
    User string `json:"user" yaml:"user"`
    Path string `json:"path,omitempty" yaml:"path,omitempty"`
}

type KinstaPlugin struct {
    Name                string `json:"name"`
    Slug                string `json:"slug"`
    Status              string `json:"status"`
    Version             string `json:"version"`
    Update              string `json:"update"`
    UpdateVersion       string `json:"updateVersion,omitempty"`
    IsVersionVulnerable bool   `json:"isVersionVulnerable"`
}

type KinstaTheme struct {
    Name          string `json:"name"`
    Slug          string `json:"slug"`
    Status        string `json:"status"`
    Version       string `json:"version"`
    Update        string `json:"update"`
    UpdateVersion string `json:"updateVersion,omitempty"`
}

type KinstaOperation struct {
    ID        string     `json:"id"`
    Status    string     `json:"status"`
    Message   string     `json:"message"`
    StartedAt time.Time  `json:"startedAt"`
    EndedAt   *time.Time `json:"endedAt,omitempty"`
}
```

### 5.4 Plugin (private repo)

```go
package domain

import "time"

type PaidPlugin struct {
    Slug          string    `json:"slug"`
    DisplayName   string    `json:"displayName"`
    LatestVersion string    `json:"latestVersion"`
    ZipPath       string    `json:"zipPath"`
    UpdatedAt     time.Time `json:"updatedAt"`
    DownloadURL   string    `json:"downloadUrl"`
}

type PluginDiff struct {
    Slug             string       `json:"slug"`
    InstalledVersion string       `json:"installedVersion"`
    AvailableVersion string       `json:"availableVersion"`
    Status           DiffStatus   `json:"status"`
    IsVulnerable     bool         `json:"isVulnerable"`
    Source           PluginSource `json:"source"`
}

type DiffStatus string

const (
    DiffUpToDate   DiffStatus = "up_to_date"
    DiffUpdate     DiffStatus = "update_available"
    DiffNotFound   DiffStatus = "not_in_repo"
    DiffVulnerable DiffStatus = "vulnerable"
)

type PluginSource string

const (
    SourcePrivateRepo PluginSource = "private_repo"
    SourceWPOrg       PluginSource = "wp_org"
    SourceUnknown     PluginSource = "unknown"
)
```

### 5.5 AWS / VPS

```go
package domain

type AWSProjectCfg struct {
    Region     string `yaml:"region"      json:"region"`
    Service    string `yaml:"service"     json:"service"`
    ResourceID string `yaml:"resource_id" json:"resourceId"`
    URL        string `yaml:"url"         json:"url"`
    Notes      string `yaml:"notes"       json:"notes"`
}

type VPSProjectCfg struct {
    Provider string    `yaml:"provider" json:"provider"`
    SSH      SSHTarget `yaml:"ssh"      json:"ssh"`
    URL      string    `yaml:"url"      json:"url"`
    Notes    string    `yaml:"notes"    json:"notes"`
}
```

---

## 6. API Contracts (Wails Bindings)

Each service is a Go struct registered with `application.NewService(...)`. Methods become callable from the frontend through generated TypeScript bindings.

### 6.1 GitService (summary)

```go
// Reads — go-git
Status(ctx, projectID) (GitStatus, error)
Log(ctx, projectID, opts LogOptions) ([]Commit, error)
Graph(ctx, projectID, opts) (CommitGraph, error)
DiffWorking(ctx, projectID, path) (FileDiff, error)
DiffStaged(ctx, projectID, path) (FileDiff, error)
DiffCommit(ctx, projectID, hash) ([]FileDiff, error)
Branches(ctx, projectID) ([]Branch, error)
Tags(ctx, projectID) ([]Tag, error)
Stashes(ctx, projectID) ([]Stash, error)
Blame(ctx, projectID, path) ([]BlameLine, error)
FileHistory(ctx, projectID, path, limit) ([]Commit, error)

// Writes — git CLI
Stage(ctx, projectID, paths) error
Unstage(ctx, projectID, paths) error
StageHunk(ctx, projectID, path, patch) error
Commit(ctx, projectID, message, opts) (Commit, error)
Push(ctx, projectID, remote, branch, opts) (string, error)
Pull(ctx, projectID, remote, branch) (string, error)
Fetch(ctx, projectID, remote) (string, error)
CheckoutBranch(ctx, projectID, name, create) error
DeleteBranch(ctx, projectID, name, remote) error
Merge(ctx, projectID, branch, opts) (string, error)
Rebase(ctx, projectID, onto, interactive) (string, error)
CherryPick(ctx, projectID, hashes) (string, error)
CreateTag(ctx, projectID, name, message) error
PushTag(ctx, projectID, name) error
StashPush(ctx, projectID, message, includeUntracked) error
StashApply(ctx, projectID, index, drop) error
SubmoduleUpdate(ctx, projectID, init) (string, error)

// Batch
BatchStatus(ctx) ([]ProjectStatusSummary, error)
BatchPull(ctx, projectIDs) (map[string]string, error)
```

### 6.2 KinstaService

```go
Sites(ctx) ([]KinstaSite, error)
Environment(ctx, envID) (KinstaEnvironment, error)
Plugins(ctx, envID) ([]KinstaPlugin, error)
Themes(ctx, envID) ([]KinstaTheme, error)
UpdatePlugin(ctx, envID, slug) (KinstaOperation, error)
PollOperation(ctx, opID) (KinstaOperation, error)
GenerateGitHubWorkflow(ctx, projectID) (string, error)
WriteGitHubWorkflow(ctx, projectID) (string, error)  // returns written path
```

### 6.3 PluginService

```go
ListPaidPlugins(ctx) ([]PaidPlugin, error)
Diff(ctx, projectID, envName) ([]PluginDiff, error)
UpdateViaSSH(ctx, projectID, envName, slug) (string, error)
RefreshIndex(ctx) error
```

### 6.4 SSHService

```go
OpenSession(ctx, projectID, envName) (sessionID string, error)
Write(ctx, sessionID, data []byte) error
Resize(ctx, sessionID, cols, rows int) error
Close(ctx, sessionID) error
// Streamed output delivered via Wails event: "ssh:<sessionID>:data"
```

### 6.5 Events (Go → Frontend)

| Event | Payload | When |
|---|---|---|
| `project:changed` | `{id}` | fsnotify detects repo change |
| `git:status:changed` | `{projectId, status}` | after any write op |
| `kinsta:operation` | `{opId, status}` | poll updates |
| `plugin:diff:updated` | `{projectId, diffs}` | after refresh |
| `ssh:<sessionID>:data` | `{base64}` | terminal output |
| `notify:vulnerable` | `{projectId, plugin, version}` | scan finds vulnerability |

---

## 7. Config Schema

### 7.1 Per-Project `.rdm/config.yml` (committed to repo)

```yaml
# .rdm/config.yml — RDM Sites Tool project config
# Lives at repo root. Safe to commit; never contains secrets.

provider: kinsta              # kinsta | aws | vps | none
display_name: "Client Website"

kinsta:
  site_id: "abc-123"
  environments:
    production:
      env_id: "prod-uuid"
      branch: main
    staging:
      env_id: "stg-uuid"
      branch: develop

aws:
  region: eu-west-1
  service: ec2                # ec2 | ecs | lightsail | s3 | amplify
  resource_id: i-0abc123
  url: https://example.com
  notes: "Reverse proxy in front of WP container."

vps:
  provider: hetzner           # hetzner | digitalocean | other
  url: https://staging.example.com
  ssh:
    host: 49.x.x.x
    port: 22
    user: ubuntu
  notes: "Legacy VPS, plan migration."

# Optional SSH override (defaults to Kinsta env SSH info)
ssh:
  host: 35.x.x.x
  port: 22
  user: deploy
  path: /www/clientsite/public
```

### 7.2 Global `~/.config/rdm/config.yml` (never committed)

```yaml
# ~/.config/rdm/config.yml
# Secrets prefixed `keychain:` are stored in macOS Keychain.

projects_roots:
  - ~/Projects
  - ~/Code/clients

editor: cursor                   # cursor | vscode | phpstorm

kinsta:
  api_key: keychain:rdm.kinsta.apiKey
  company_id: "..."

plugin_repo:
  github_token: keychain:rdm.github.token
  repo: "org/wp-paid-plugins"
  ref: main

notifications:
  enable_vulnerability_alerts: true
  scan_interval_minutes: 60

git:
  default_remote: origin
  prune_on_fetch: true
```

### 7.3 Validation Rules

- Both files validated against Go struct JSON Schemas at startup.
- Unknown keys → warning (forward compat).
- Required: `kinsta.site_id` when provider is kinsta; `vps.ssh.host` when provider is vps.
- Secrets in global config must be `keychain:<key>` or empty — raw secret strings are rejected with a migration hint.

---

## 8. Private Plugin Repo Integration

### 8.1 Repository Layout (your private GitHub repo)

```
wp-paid-plugins/
├── manifest.yml
├── advanced-custom-fields-pro/
│   ├── 6.3.4.zip
│   └── 6.3.5.zip
├── gravityforms/
│   └── 2.8.11.zip
└── wp-rocket/
    └── 3.16.0.zip
```

### 8.2 `manifest.yml` (source of truth for versions)

```yaml
plugins:
  - slug: advanced-custom-fields-pro
    display_name: Advanced Custom Fields Pro
    versions:
      - version: 6.3.5
        path: advanced-custom-fields-pro/6.3.5.zip
        released_at: 2026-04-12
  - slug: gravityforms
    display_name: Gravity Forms
    versions:
      - version: 2.8.11
        path: gravityforms/2.8.11.zip
        released_at: 2026-04-22
```

### 8.3 Sync Algorithm

1. `PluginService.RefreshIndex()` fetches `manifest.yml` via GitHub Contents API.
2. Index cached in `~/Library/Caches/rdm-sites-tool/plugin-index.json` with ETag.
3. `Diff(projectID, envName)` calls `kinsta.Plugins(envID)` and joins on slug.
4. Status rules:
   - `installed == latest` → `up_to_date`
   - `installed < latest` → `update_available`
   - slug not in manifest → `not_in_repo` (wp.org plugin)
   - `kinsta.is_version_vulnerable == true` → `vulnerable` (overrides above)

### 8.4 Update via SSH

```bash
# Tool runs these steps over the SSH session:
cd /www/site/public
curl -H "Authorization: token <gh-token>" -L <raw-zip-url> -o /tmp/plugin.zip
wp plugin install /tmp/plugin.zip --activate --force
rm /tmp/plugin.zip
```

Output streams back to the embedded terminal in real time.

### 8.5 Auto-Generated GitHub Workflow

`KinstaService.WriteGitHubWorkflow(projectID)` writes `.github/workflows/check-updates.yml` with env IDs interpolated from `.rdm/config.yml`:

```yaml
name: Check Kinsta Plugin Updates
on:
  schedule:
    - cron: "0 6 * * 1"   # Monday 06:00 UTC
  workflow_dispatch:
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - name: Check production plugins
        env:
          KINSTA_API_KEY: ${{ secrets.KINSTA_API_KEY }}
          ENV_ID: prod-uuid
        run: |
          curl -sS \
            -H "Authorization: Bearer $KINSTA_API_KEY" \
            "https://api.kinsta.com/v2/sites/environments/$ENV_ID/wp-plugins" \
            | jq '.plugins[] | select(.update == "available" or .is_version_vulnerable == true)'
```

---

## 9. macOS-Specific Considerations

### 9.1 Code Signing & Notarization

App is **not sandboxed** (must read arbitrary folders, run `git`).

`entitlements.plist`:
```xml
<dict>
  <key>com.apple.security.app-sandbox</key><false/>
  <key>com.apple.security.cs.allow-unsigned-executable-memory</key><true/>
  <key>com.apple.security.network.client</key><true/>
  <key>com.apple.security.files.user-selected.read-write</key><true/>
  <key>com.apple.security.files.bookmarks.app-scope</key><true/>
</dict>
```

Build pipeline (`make sign`):
1. `wails build -platform darwin/universal`
2. `codesign --deep --force --options runtime --entitlements ... --sign "Developer ID Application: ..."`
3. `xcrun notarytool submit ... --wait`
4. `xcrun stapler staple RDMSitesTool.app`
5. `create-dmg` → `RDMSitesTool.dmg`

### 9.2 Keychain for Secrets

Use `github.com/zalando/go-keyring`. Service name: `nl.nobears.kinsta-updater`. Keys die nog onder de oude naam `nl.micromanage.rdm-sites-tool` staan, worden bij het opstarten eenmalig overgezet door `config.MigrateKeychainService()`. On first run, user pastes Kinsta API key and GitHub token; both stored in Keychain. Config references them as `keychain:rdm.kinsta.apiKey`.

### 9.3 Native Notifications

v1: `osascript -e 'display notification ...'` (no permission prompts, works immediately).
Phase 3: `UserNotifications.framework` via cgo for action buttons ("Update now", "Dismiss").

### 9.4 Native Menu Bar

Wails v3 supports native macOS menus. Standard structure: App / File / Repository / View / Window / Help. Repository menu mirrors common actions with shortcuts: `⌘P` push, `⌘⇧F` fetch, `⌘⇧P` pull.

### 9.5 Multiple Project Roots

`projects_roots` in global config is a list. Useful for teams that keep work split across `~/Projects` and `~/Code/clients`.

---

## 10. Development Setup

### 10.1 Prerequisites

- macOS 13+ (Ventura or newer)
- Xcode Command Line Tools: `xcode-select --install`
- Go 1.22+: `brew install go`
- Node.js 20+: `brew install node`
- Wails v3: `go install github.com/wailsapp/wails/v3/cmd/wails3@latest`
- Git 2.40+: `brew install git`
- Apple Developer ID certificate (for signed builds)

### 10.2 First Run

```bash
git clone <repo> RDM-Sites-tool && cd RDM-Sites-tool
make bootstrap      # go mod download + npm install
cp .env.example .env
wails3 dev          # hot-reload: Go + Vite
```

### 10.3 Makefile Targets

| Target | Description |
|---|---|
| `bootstrap` | Install all deps |
| `dev` | `wails3 dev` with hot reload |
| `build` | `wails3 build -platform darwin/universal` |
| `sign` | Codesign + notarize + staple |
| `dmg` | Produce distributable `.dmg` |
| `test` | `go test ./... && npm test` |
| `lint` | `golangci-lint run && npm run lint` |
| `gen-bindings` | `wails3 generate bindings` (typed TS) |

### 10.4 Testing Strategy

- **Unit (Go)**: table-driven tests, mocked HTTP via `httptest.Server`, fixture git repos under `tests/fixtures/`.
- **Integration**: spin temp git repos per test, run write ops end-to-end through `gitcli`.
- **Frontend**: Vitest for hooks + components; Playwright component mode for CommitGraph.
- **E2E**: Wails dev mode + Playwright driving the WebView.
- Coverage target ≥ 80% for `internal/services` and `internal/adapters`.

---

## 11. Frontend Component Hierarchy

```
<App>
└── <Shell>
    ├── <Sidebar>
    │   ├── <GlobalSearch />
    │   └── <ProjectList>
    │       └── <ProjectRow />        # provider badge, branch, status dots
    ├── <Main>
    │   └── <ProjectDetail>
    │       ├── <ProjectHeader>       # name, branch picker, quick-open
    │       └── <Tabs>
    │           ├── "Changes"   → <StagingArea> + <DiffViewer>
    │           ├── "History"   → <CommitGraph> + <CommitList> + <DiffViewer>
    │           ├── "Branches"  → <BranchList> + <TagsPanel> + <StashPanel>
    │           ├── "Kinsta"    → <EnvironmentCard> × N + <PluginUpdatesTable>
    │           ├── "Deploy"    → <AwsInfo> | <VpsInfo>
    │           ├── "Terminal"  → <SshTerminal>
    │           └── "Blame"     → <BlameView>
    └── <StatusBar>               # batch ops, background tasks, network state
```

---

## 12. Architecture Decision Records

| ADR | Decision |
|---|---|
| ADR-001 | go-git for reads, git CLI for writes — performance + correctness on complex ops |
| ADR-002 | Wails v3 over Tauri/Electron — Go-native, single binary, smaller footprint |
| ADR-003 | shadcn/ui over MUI — lower bundle size, easier theming |
| ADR-004 | Keychain for secrets — no encrypted file, no plaintext risk |
| ADR-005 | Per-project `.rdm/config.yml` committed — any teammate clone sees same metadata |
| ADR-006 | SSH path for paid plugin updates — simpler than SFTP upload to Kinsta API |

---

## 13. Open Questions

1. Should `.rdm/config.yml` have a `rdm_schema_version: 1` field for future migrations?
2. Multi-user conflict: if two devs trigger a Kinsta update simultaneously, track in-flight operation per env and disable the button.
3. Telemetry/crash reporting — out of scope for v1.
4. Should the tool support detecting projects **without** `.rdm/config.yml` (Git repos only, no deployment config)?
