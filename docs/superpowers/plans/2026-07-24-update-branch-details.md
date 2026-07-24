# Update-branch details (PR + tool) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Toon per automated update-branch welke updates zijn uitgevoerd (WordPress core/plugins/thema's + npm minor/patch) en welke npm-majors beschikbaar zijn maar bewust niet worden toegepast — zowel in de PR-body als uitklapbaar in de tool.

**Architecture:** De reusable workflow schrijft een gestructureerd `.updates.json`-manifest in de update-branch (naast het bestaande `.wp-update-log`). De parsing/berekening zit in een los, unit-getest Node-module dat de github-script-stap aanroept. De Go-backend leest het manifest (met fallback naar `.wp-update-log` + `package.json`-diff) en de React-frontend rendert het uitklapbaar. De tool blijft read-only.

**Tech Stack:** GitHub Actions (`actions/github-script@v7`), Node 24 (`node:test`), Go 1.25 (`go test`, `gitcli`-adapter), React + TypeScript + Wails v3 bindings, Tailwind.

## Global Constraints

- npm bumpen gebeurt alléén met `--target minor`; majors worden nooit toegepast, enkel gerapporteerd.
- De tool voert uitsluitend read-only git uit: `git show` / `git diff`. Geen writes in klant-repo's.
- Nieuwe Node-code: CommonJS (`module.exports` / `require`), zodat `actions/github-script` het met `require('./scripts/updates/manifest.js')` kan laden.
- Go: wrap errors met `fmt.Errorf("...: %w", err)`; table-driven tests; package `services`.
- Frontend heeft geen testframework — nieuwe UI wordt visueel geverifieerd in de draaiende dev-app (poort 9245). Geen nieuw testframework introduceren.
- Manifest-veldnamen exact: `generatedAt`, `wordpress.core[].{version,updateType}`, `wordpress.plugins[]`/`themes[].{name,from,to}`, `npm.applied[].{name,from,to,type}`, `npm.availableMajors[].{name,from,to}`.

---

## Phase 1 — Node parse/compute-module (unit-getest)

### Task 1: WP-CLI output parser

**Files:**
- Create: `scripts/updates/manifest.js`
- Test: `scripts/updates/manifest.test.js`

**Interfaces:**
- Produces: `parseWpUpdates(stdout: string) => { core: {version,updateType}[], plugins: {name,from,to}[], themes: {name,from,to}[] }`

- [ ] **Step 1: Write the failing test**

```js
// scripts/updates/manifest.test.js
const { test } = require('node:test')
const assert = require('node:assert/strict')
const { parseWpUpdates } = require('./manifest')

const SAMPLE = `=== WORDPRESS CORE ===
version\tupdate_type\tpackage_url
6.9.5\tminor\thttps://downloads.wordpress.org/release/nl_NL/wordpress-6.9.5.zip
7.0.2\tmajor\thttps://downloads.wordpress.org/release/nl_NL/wordpress-7.0.2.zip
=== PLUGINS ===
name\tversion\tupdate_version
acfml\t2.2.3\t2.2.4
wp-rocket\t3.21.1\t3.23
=== THEMES ===
name\tversion\tupdate_version
===============================================
✅ Successfully executed commands to all hosts.`

test('parseWpUpdates extracts core/plugins/themes', () => {
  const r = parseWpUpdates(SAMPLE)
  assert.deepEqual(r.core, [
    { version: '6.9.5', updateType: 'minor' },
    { version: '7.0.2', updateType: 'major' },
  ])
  assert.deepEqual(r.plugins, [
    { name: 'acfml', from: '2.2.3', to: '2.2.4' },
    { name: 'wp-rocket', from: '3.21.1', to: '3.23' },
  ])
  assert.deepEqual(r.themes, [])
})

test('parseWpUpdates handles no-update output', () => {
  const r = parseWpUpdates('=== WORDPRESS CORE ===\nSuccess: WordPress is at the latest version.\n=== PLUGINS ===\n=== THEMES ===')
  assert.deepEqual(r, { core: [], plugins: [], themes: [] })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `node --test scripts/updates/`
Expected: FAIL — `Cannot find module './manifest'`

- [ ] **Step 3: Write minimal implementation**

```js
// scripts/updates/manifest.js
'use strict'

// Split the SSH stdout into its === SECTION === blocks.
function sections(stdout) {
  const out = {}
  let current = null
  for (const rawLine of String(stdout).split('\n')) {
    const line = rawLine.replace(/\r$/, '')
    const header = line.match(/^===\s*(.+?)\s*===$/)
    if (header) {
      const name = header[1].toUpperCase()
      // Ignore the trailing separator line (=====...) which has empty name.
      if (name && !/^=+$/.test(name)) {
        current = name
        out[current] = []
      } else {
        current = null
      }
      continue
    }
    if (current) out[current].push(line)
  }
  return out
}

// Keep only tab-separated data rows, dropping headers and status lines.
function dataRows(lines, headerFirstCol) {
  return (lines || [])
    .filter((l) => l.includes('\t'))
    .map((l) => l.split('\t'))
    .filter((cols) => cols[0] && cols[0] !== headerFirstCol)
}

function parseWpUpdates(stdout) {
  const s = sections(stdout)
  const core = dataRows(s['WORDPRESS CORE'], 'version').map((c) => ({
    version: c[0],
    updateType: c[1] || '',
  }))
  const toPkg = (rows) => rows.map((c) => ({ name: c[0], from: c[1] || '', to: c[2] || '' }))
  return {
    core,
    plugins: toPkg(dataRows(s['PLUGINS'], 'name')),
    themes: toPkg(dataRows(s['THEMES'], 'name')),
  }
}

module.exports = { parseWpUpdates }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `node --test scripts/updates/`
Expected: PASS (2 tests)

- [ ] **Step 5: Commit**

```bash
git add scripts/updates/manifest.js scripts/updates/manifest.test.js
git commit -m "feat(updates): parse WP-CLI update output into structured data"
```

---

### Task 2: npm updates compute (applied + availableMajors)

**Files:**
- Modify: `scripts/updates/manifest.js`
- Test: `scripts/updates/manifest.test.js`

**Interfaces:**
- Consumes: `parseWpUpdates` (Task 1)
- Produces:
  - `classifyBump(from: string, to: string) => 'major'|'minor'|'patch'`
  - `computeNpmUpdates({ current: Record<string,string>, minor: Record<string,string>, latest: Record<string,string> }) => { applied: {name,from,to,type}[], availableMajors: {name,from,to}[] }`
  - `current`/`minor`/`latest` are name→version-range maps (`minor` = ncu `--jsonUpgraded --target minor`, `latest` = ncu `--jsonUpgraded`).

- [ ] **Step 1: Write the failing test**

```js
// append to scripts/updates/manifest.test.js
const { classifyBump, computeNpmUpdates } = require('./manifest')

test('classifyBump distinguishes major/minor/patch ignoring range prefixes', () => {
  assert.equal(classifyBump('^1.99.0', '^1.101.7'), 'minor')
  assert.equal(classifyBump('10.5.0', '10.5.4'), 'patch')
  assert.equal(classifyBump('9.39.2', '10.7.0'), 'major')
})

test('computeNpmUpdates splits applied minor/patch from available majors', () => {
  const r = computeNpmUpdates({
    current: { sass: '^1.99.0', eslint: '9.39.2', autoprefixer: '10.5.0' },
    minor: { sass: '^1.101.7', autoprefixer: '10.5.4' },
    latest: { sass: '^1.101.7', eslint: '10.7.0', autoprefixer: '10.5.4' },
  })
  assert.deepEqual(r.applied, [
    { name: 'sass', from: '^1.99.0', to: '^1.101.7', type: 'minor' },
    { name: 'autoprefixer', from: '10.5.0', to: '10.5.4', type: 'patch' },
  ])
  assert.deepEqual(r.availableMajors, [
    { name: 'eslint', from: '9.39.2', to: '10.7.0' },
  ])
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `node --test scripts/updates/`
Expected: FAIL — `classifyBump is not a function`

- [ ] **Step 3: Write minimal implementation**

```js
// add to scripts/updates/manifest.js, before module.exports
function parseSemver(v) {
  const m = String(v).replace(/^[^\d]*/, '').match(/^(\d+)\.(\d+)\.(\d+)/)
  if (!m) return null
  return { major: +m[1], minor: +m[2], patch: +m[3] }
}

function classifyBump(from, to) {
  const a = parseSemver(from)
  const b = parseSemver(to)
  if (!a || !b) return 'minor'
  if (b.major !== a.major) return 'major'
  if (b.minor !== a.minor) return 'minor'
  return 'patch'
}

function computeNpmUpdates({ current = {}, minor = {}, latest = {} }) {
  const applied = Object.keys(minor).map((name) => ({
    name,
    from: current[name] ?? '',
    to: minor[name],
    type: classifyBump(current[name] ?? '', minor[name]),
  }))
  const availableMajors = Object.keys(latest)
    .filter((name) => classifyBump(current[name] ?? '', latest[name]) === 'major')
    .map((name) => ({ name, from: current[name] ?? '', to: latest[name] }))
  return { applied, availableMajors }
}
```

Update the export line:

```js
module.exports = { parseWpUpdates, classifyBump, computeNpmUpdates }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `node --test scripts/updates/`
Expected: PASS (4 tests)

- [ ] **Step 5: Commit**

```bash
git add scripts/updates/manifest.js scripts/updates/manifest.test.js
git commit -m "feat(updates): compute applied npm updates and available majors"
```

---

### Task 3: build manifest + render PR sections

**Files:**
- Modify: `scripts/updates/manifest.js`
- Test: `scripts/updates/manifest.test.js`

**Interfaces:**
- Consumes: `parseWpUpdates`, `computeNpmUpdates` (Tasks 1-2)
- Produces:
  - `buildManifest({ generatedAt, wordpress, npm }) => object` (the `.updates.json` shape)
  - `renderNpmMajorsSection(availableMajors: {name,from,to}[]) => string` (empty string when none)

- [ ] **Step 1: Write the failing test**

```js
// append to scripts/updates/manifest.test.js
const { buildManifest, renderNpmMajorsSection } = require('./manifest')

test('buildManifest assembles the manifest shape', () => {
  const m = buildManifest({
    generatedAt: '2026-07-20T09:00:57Z',
    wordpress: { core: [{ version: '6.9.5', updateType: 'minor' }], plugins: [], themes: [] },
    npm: { applied: [{ name: 'sass', from: '1', to: '1.1', type: 'minor' }], availableMajors: [] },
  })
  assert.equal(m.generatedAt, '2026-07-20T09:00:57Z')
  assert.deepEqual(m.wordpress.core, [{ version: '6.9.5', updateType: 'minor' }])
  assert.deepEqual(m.npm.applied, [{ name: 'sass', from: '1', to: '1.1', type: 'minor' }])
  assert.deepEqual(m.npm.availableMajors, [])
})

test('renderNpmMajorsSection lists majors, or empty when none', () => {
  assert.equal(renderNpmMajorsSection([]), '')
  const s = renderNpmMajorsSection([{ name: 'eslint', from: '9.39.2', to: '10.7.0' }])
  assert.match(s, /Beschikbare major updates/)
  assert.match(s, /NIET automatisch uitgevoerd/)
  assert.match(s, /eslint.*9\.39\.2.*10\.7\.0/)
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `node --test scripts/updates/`
Expected: FAIL — `buildManifest is not a function`

- [ ] **Step 3: Write minimal implementation**

```js
// add to scripts/updates/manifest.js, before module.exports
function buildManifest({ generatedAt, wordpress, npm }) {
  return {
    generatedAt,
    wordpress: {
      core: wordpress.core || [],
      plugins: wordpress.plugins || [],
      themes: wordpress.themes || [],
    },
    npm: {
      applied: (npm && npm.applied) || [],
      availableMajors: (npm && npm.availableMajors) || [],
    },
  }
}

function renderNpmMajorsSection(availableMajors) {
  if (!availableMajors || availableMajors.length === 0) return ''
  const lines = availableMajors.map((m) => `- \`${m.name}\` ${m.from} → ${m.to}`).join('\n')
  return [
    '### ⚠️ Beschikbare major updates — NIET automatisch uitgevoerd (meerwerk)',
    '',
    'Deze npm-majors zijn beschikbaar maar bewust niet toegepast. Handmatig oppakken indien gewenst.',
    '',
    lines,
    '',
  ].join('\n')
}
```

Update the export line:

```js
module.exports = { parseWpUpdates, classifyBump, computeNpmUpdates, buildManifest, renderNpmMajorsSection }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `node --test scripts/updates/`
Expected: PASS (6 tests)

- [ ] **Step 5: Commit**

```bash
git add scripts/updates/manifest.js scripts/updates/manifest.test.js
git commit -m "feat(updates): build manifest and render npm-majors PR section"
```

---

## Phase 2 — Wire module into the workflow

### Task 4: emit npm JSON + write manifest + majors section in check-updates.yml

**Files:**
- Modify: `.github/workflows/check-updates.yml:63-193`

**Interfaces:**
- Consumes: `scripts/updates/manifest.js` (Phase 1) via `require` in the github-script step.
- Note: not unit-testable in this repo (needs Actions runner + SSH secrets). Verified by dispatching the caller workflow after merge (see Task 4 Step 4).

- [ ] **Step 1: Replace the npm step to also capture current/minor/latest JSON**

Replace the `Controleer NPM updates (minor + patch)` step body (lines 63-84) so it writes three JSON files next to the repo root and still applies only the minor target:

```yaml
      - name: Controleer NPM updates (minor + patch)
        id: check_npm
        if: hashFiles('package.json') != ''
        shell: bash
        run: |
          set -euo pipefail
          # Snapshot huidige versies (dep + devDep) vóór enige wijziging.
          node -e "const p=require('./package.json');console.log(JSON.stringify({...(p.dependencies||{}),...(p.devDependencies||{})}))" > /tmp/npm-current.json
          # Read-only passes: welke minor/patch en welke latest (voor majors).
          npx --yes npm-check-updates@latest --jsonUpgraded --target minor > /tmp/npm-minor.json || echo '{}' > /tmp/npm-minor.json
          npx --yes npm-check-updates@latest --jsonUpgraded > /tmp/npm-latest.json || echo '{}' > /tmp/npm-latest.json
          # Pas alleen minor/patch daadwerkelijk toe.
          npx --yes npm-check-updates@latest -u --target minor --color false
          npm install --package-lock-only --ignore-scripts
          if git diff --quiet -- package.json package-lock.json; then
            echo "has_npm_updates=false" >> "$GITHUB_OUTPUT"
            echo "Geen NPM updates."
          else
            echo "has_npm_updates=true" >> "$GITHUB_OUTPUT"
            git diff --stat -- package.json package-lock.json
          fi
```

- [ ] **Step 2: In the github-script step, build the manifest, add the majors section, and commit `.updates.json`**

In the `Maak update branch en PR aan` step (lines 86-193), add near the top of the `script:` (after `const fs = require('fs')`):

```js
            const {
              parseWpUpdates, computeNpmUpdates, buildManifest, renderNpmMajorsSection,
            } = require('./scripts/updates/manifest.js');

            const readJson = (p) => { try { return JSON.parse(fs.readFileSync(p, 'utf8')); } catch { return {}; } };
            const wordpress = hasWp ? parseWpUpdates(updateOutput) : { core: [], plugins: [], themes: [] };
            const npm = hasNpm
              ? computeNpmUpdates({
                  current: readJson('/tmp/npm-current.json'),
                  minor: readJson('/tmp/npm-minor.json'),
                  latest: readJson('/tmp/npm-latest.json'),
                })
              : { applied: [], availableMajors: [] };
            const manifest = buildManifest({ generatedAt: new Date().toISOString(), wordpress, npm });
```

Add the manifest blob to the `tree` array (after the `hasNpm` block, ~line 142):

```js
            {
              const { data: blob } = await github.rest.git.createBlob({
                owner: context.repo.owner,
                repo: context.repo.repo,
                content: JSON.stringify(manifest, null, 2) + '\n',
                encoding: 'utf-8',
              });
              tree.push({ path: '.updates.json', mode: '100644', type: 'blob', sha: blob.sha });
            }
```

Extend the PR body (after the `hasNpm` body block, ~line 178):

```js
            body += renderNpmMajorsSection(manifest.npm.availableMajors);
```

- [ ] **Step 3: Guard `tree` is non-empty**

The `Maak update branch en PR aan` step only runs when `has_updates || has_npm_updates` is true, so `tree` always has at least the WP log or npm files plus the manifest — no extra guard needed. Verify the `if:` on the step (line 87) is unchanged.

- [ ] **Step 4: Manual verification (after merge to release/main)**

```bash
# Vanuit een caller-repo met root package.json (bv. web-vanluykennl):
gh workflow run check-wp-updates-on-kinsta.yml
# wacht op run, dan:
gh run list --workflow=check-wp-updates-on-kinsta.yml --limit 1
```

Expected: nieuwe `automated/updates-*` branch bevat `.updates.json`; PR-body toont de majors-sectie wanneer er npm-majors zijn.

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/check-updates.yml
git commit -m "feat(workflow): write .updates.json manifest and npm-majors PR section"
```

---

## Phase 3 — Go backend

### Task 5: UpdateDetail types + manifest parsing

**Files:**
- Modify: `internal/services/git_service.go` (add types near `UpdateBranch`, ~line 490)
- Test: `internal/services/update_detail_test.go` (create)

**Interfaces:**
- Produces:
  ```go
  type PackageUpdate struct { Name, From, To, Type string }  // json: name,from,to,type(omitempty)
  type WPCoreUpdate struct { Version, UpdateType string }     // json: version,updateType
  type UpdateDetail struct {
      Source string; GeneratedAt string
      WPCore []WPCoreUpdate; WPPlugins, WPThemes []PackageUpdate
      NpmApplied, NpmAvailableMajors []PackageUpdate
  }
  func parseUpdateManifest(data []byte) (*UpdateDetail, error)
  ```

- [ ] **Step 1: Write the failing test**

```go
// internal/services/update_detail_test.go
package services

import "testing"

func TestParseUpdateManifest(t *testing.T) {
	data := []byte(`{
	  "generatedAt": "2026-07-20T09:00:57Z",
	  "wordpress": {
	    "core": [{"version":"7.0.2","updateType":"major"}],
	    "plugins": [{"name":"acfml","from":"2.2.3","to":"2.2.4"}],
	    "themes": []
	  },
	  "npm": {
	    "applied": [{"name":"sass","from":"1.99.0","to":"1.101.7","type":"minor"}],
	    "availableMajors": [{"name":"eslint","from":"9.39.2","to":"10.7.0"}]
	  }
	}`)
	d, err := parseUpdateManifest(data)
	if err != nil {
		t.Fatalf("parseUpdateManifest: %v", err)
	}
	if d.Source != "manifest" {
		t.Errorf("Source = %q, want manifest", d.Source)
	}
	if len(d.WPCore) != 1 || d.WPCore[0].UpdateType != "major" {
		t.Errorf("WPCore = %+v", d.WPCore)
	}
	if len(d.WPPlugins) != 1 || d.WPPlugins[0].Name != "acfml" {
		t.Errorf("WPPlugins = %+v", d.WPPlugins)
	}
	if len(d.NpmApplied) != 1 || d.NpmApplied[0].Type != "minor" {
		t.Errorf("NpmApplied = %+v", d.NpmApplied)
	}
	if len(d.NpmAvailableMajors) != 1 || d.NpmAvailableMajors[0].Name != "eslint" {
		t.Errorf("NpmAvailableMajors = %+v", d.NpmAvailableMajors)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/services/ -run TestParseUpdateManifest`
Expected: FAIL — `undefined: parseUpdateManifest`

- [ ] **Step 3: Write minimal implementation**

```go
// add to internal/services/git_service.go, after the UpdateBranch type (~line 495)

// PackageUpdate is a single package version change (or availability).
type PackageUpdate struct {
	Name string `json:"name"`
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type,omitempty"` // "minor" | "patch" (npm applied)
}

// WPCoreUpdate is a single WordPress core version availability.
type WPCoreUpdate struct {
	Version    string `json:"version"`
	UpdateType string `json:"updateType"` // "minor" | "major"
}

// UpdateDetail is the fully resolved set of updates inside an update branch.
type UpdateDetail struct {
	Source             string          `json:"source"` // "manifest" | "fallback"
	GeneratedAt        string          `json:"generatedAt,omitempty"`
	WPCore             []WPCoreUpdate  `json:"wpCore"`
	WPPlugins          []PackageUpdate `json:"wpPlugins"`
	WPThemes           []PackageUpdate `json:"wpThemes"`
	NpmApplied         []PackageUpdate `json:"npmApplied"`
	NpmAvailableMajors []PackageUpdate `json:"npmAvailableMajors"`
}

// manifestFile is the on-branch .updates.json shape.
type manifestFile struct {
	GeneratedAt string `json:"generatedAt"`
	Wordpress   struct {
		Core    []WPCoreUpdate  `json:"core"`
		Plugins []PackageUpdate `json:"plugins"`
		Themes  []PackageUpdate `json:"themes"`
	} `json:"wordpress"`
	Npm struct {
		Applied         []PackageUpdate `json:"applied"`
		AvailableMajors []PackageUpdate `json:"availableMajors"`
	} `json:"npm"`
}

func parseUpdateManifest(data []byte) (*UpdateDetail, error) {
	var m manifestFile
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &UpdateDetail{
		Source:             "manifest",
		GeneratedAt:        m.GeneratedAt,
		WPCore:             m.Wordpress.Core,
		WPPlugins:          m.Wordpress.Plugins,
		WPThemes:           m.Wordpress.Themes,
		NpmApplied:         m.Npm.Applied,
		NpmAvailableMajors: m.Npm.AvailableMajors,
	}, nil
}
```

Ensure `encoding/json` is imported in `git_service.go` (add to the import block if missing).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/services/ -run TestParseUpdateManifest`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/services/git_service.go internal/services/update_detail_test.go
git commit -m "feat(tool): UpdateDetail types and .updates.json manifest parser"
```

---

### Task 6: fallback parsers + GetUpdateBranchDetail

**Files:**
- Modify: `internal/services/git_service.go`
- Test: `internal/services/update_detail_test.go`

**Interfaces:**
- Consumes: `parseUpdateManifest`, `PackageUpdate`, `WPCoreUpdate`, `UpdateDetail` (Task 5); `gitcli.Run` (`internal/adapters/gitcli`).
- Produces:
  - `parseWpUpdateLog(log string) (core []WPCoreUpdate, plugins, themes []PackageUpdate)`
  - `npmAppliedFromPackageJSON(before, after string) []PackageUpdate`
  - `func (s *GitService) GetUpdateBranchDetail(projectID, shortName string) (*UpdateDetail, error)`

- [ ] **Step 1: Write the failing tests (pure parsers)**

```go
// append to internal/services/update_detail_test.go
func TestParseWpUpdateLog(t *testing.T) {
	log := "WordPress update check uitgevoerd op: x\n\n=== WORDPRESS CORE ===\n" +
		"version\tupdate_type\tpackage_url\n7.0.2\tmajor\thttps://x\n" +
		"=== PLUGINS ===\nname\tversion\tupdate_version\nacfml\t2.2.3\t2.2.4\n" +
		"=== THEMES ===\nname\tversion\tupdate_version\n"
	core, plugins, themes := parseWpUpdateLog(log)
	if len(core) != 1 || core[0].UpdateType != "major" {
		t.Errorf("core = %+v", core)
	}
	if len(plugins) != 1 || plugins[0].To != "2.2.4" {
		t.Errorf("plugins = %+v", plugins)
	}
	if len(themes) != 0 {
		t.Errorf("themes = %+v", themes)
	}
}

func TestNpmAppliedFromPackageJSON(t *testing.T) {
	before := `{"dependencies":{"sass":"^1.99.0"},"devDependencies":{"webpack":"5.106.2"}}`
	after := `{"dependencies":{"sass":"^1.101.7"},"devDependencies":{"webpack":"5.109.0"}}`
	got := npmAppliedFromPackageJSON(before, after)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (%+v)", len(got), got)
	}
	m := map[string]PackageUpdate{}
	for _, u := range got {
		m[u.Name] = u
	}
	if m["sass"].From != "^1.99.0" || m["sass"].To != "^1.101.7" {
		t.Errorf("sass = %+v", m["sass"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/services/ -run 'TestParseWpUpdateLog|TestNpmAppliedFromPackageJSON'`
Expected: FAIL — `undefined: parseWpUpdateLog`

- [ ] **Step 3: Write minimal implementation of the parsers**

```go
// add to internal/services/git_service.go

// parseWpUpdateLog extracts update tables from a .wp-update-log text file.
func parseWpUpdateLog(log string) (core []WPCoreUpdate, plugins, themes []PackageUpdate) {
	core, plugins, themes = []WPCoreUpdate{}, []PackageUpdate{}, []PackageUpdate{}
	section := ""
	for _, raw := range strings.Split(log, "\n") {
		line := strings.TrimRight(raw, "\r")
		switch {
		case strings.HasPrefix(line, "=== WORDPRESS CORE ==="):
			section = "core"
			continue
		case strings.HasPrefix(line, "=== PLUGINS ==="):
			section = "plugins"
			continue
		case strings.HasPrefix(line, "=== THEMES ==="):
			section = "themes"
			continue
		case strings.HasPrefix(line, "==="):
			section = ""
			continue
		}
		if section == "" || !strings.Contains(line, "\t") {
			continue
		}
		cols := strings.Split(line, "\t")
		switch section {
		case "core":
			if cols[0] == "version" {
				continue
			}
			c := WPCoreUpdate{Version: cols[0]}
			if len(cols) > 1 {
				c.UpdateType = cols[1]
			}
			core = append(core, c)
		case "plugins", "themes":
			if cols[0] == "name" {
				continue
			}
			p := PackageUpdate{Name: cols[0]}
			if len(cols) > 1 {
				p.From = cols[1]
			}
			if len(cols) > 2 {
				p.To = cols[2]
			}
			if section == "plugins" {
				plugins = append(plugins, p)
			} else {
				themes = append(themes, p)
			}
		}
	}
	return core, plugins, themes
}

// npmAppliedFromPackageJSON diffs two package.json blobs into changed deps.
func npmAppliedFromPackageJSON(before, after string) []PackageUpdate {
	deps := func(s string) map[string]string {
		var p struct {
			Dependencies    map[string]string `json:"dependencies"`
			DevDependencies map[string]string `json:"devDependencies"`
		}
		_ = json.Unmarshal([]byte(s), &p)
		out := map[string]string{}
		for k, v := range p.Dependencies {
			out[k] = v
		}
		for k, v := range p.DevDependencies {
			out[k] = v
		}
		return out
	}
	b, a := deps(before), deps(after)
	names := make([]string, 0, len(a))
	for name, av := range a {
		if bv, ok := b[name]; ok && bv != av {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	out := make([]PackageUpdate, 0, len(names))
	for _, name := range names {
		out = append(out, PackageUpdate{Name: name, From: b[name], To: a[name]})
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/services/ -run 'TestParseWpUpdateLog|TestNpmAppliedFromPackageJSON'`
Expected: PASS

- [ ] **Step 5: Implement GetUpdateBranchDetail (integration glue)**

```go
// add to internal/services/git_service.go

// GetUpdateBranchDetail resolves all updates carried by an update branch,
// preferring the .updates.json manifest and falling back to the text log +
// package.json diff. Read-only.
func (s *GitService) GetUpdateBranchDetail(projectID, shortName string) (*UpdateDetail, error) {
	path, err := s.pathFor(projectID)
	if err != nil {
		return nil, fmt.Errorf("update detail: %w", err)
	}
	ctx, cancel := s.ctxDefault()
	defer cancel()

	// Resolve a usable ref: prefer the remote copy, fall back to a local branch.
	ref := "origin/" + shortName
	if _, err := gitcli.Run(ctx, path, "rev-parse", "--verify", ref); err != nil {
		ref = shortName
	}

	// 1. Manifest path.
	if out, err := gitcli.Run(ctx, path, "show", ref+":.updates.json"); err == nil && out != "" {
		if d, err := parseUpdateManifest([]byte(out)); err == nil {
			return d, nil
		}
	}

	// 2. Fallback: text log + package.json diff (parent vs ref).
	d := &UpdateDetail{
		Source: "fallback", WPCore: []WPCoreUpdate{},
		WPPlugins: []PackageUpdate{}, WPThemes: []PackageUpdate{},
		NpmApplied: []PackageUpdate{}, NpmAvailableMajors: []PackageUpdate{},
	}
	if log, err := gitcli.Run(ctx, path, "show", ref+":.wp-update-log"); err == nil && log != "" {
		d.WPCore, d.WPPlugins, d.WPThemes = parseWpUpdateLog(log)
	}
	after, errA := gitcli.Run(ctx, path, "show", ref+":package.json")
	before, errB := gitcli.Run(ctx, path, "show", ref+"~1:package.json")
	if errA == nil && errB == nil && after != "" && before != "" {
		d.NpmApplied = npmAppliedFromPackageJSON(before, after)
	}
	return d, nil
}
```

- [ ] **Step 6: Verify the whole package builds and tests pass**

Run: `go build ./... && go test ./internal/services/`
Expected: build OK; tests PASS

- [ ] **Step 7: Commit**

```bash
git add internal/services/git_service.go internal/services/update_detail_test.go
git commit -m "feat(tool): GetUpdateBranchDetail with manifest + text-log/package.json fallback"
```

---

## Phase 4 — Frontend (verified in live dev app)

### Task 7: expandable update-branch rows in UpdatesTab

**Files:**
- Modify: `frontend/src/components/UpdatesTab.tsx`
- Regenerate: Wails bindings (adds `GetUpdateBranchDetail` + `UpdateDetail` type)

**Interfaces:**
- Consumes: `Services.GitService.GetUpdateBranchDetail(projectId, shortName)` returning `UpdateDetail` (Task 6).

- [ ] **Step 1: Regenerate bindings so the new method/type are available**

Run: `wails3 generate bindings -config ./build/config.yml` (or restart `task dev`, which regenerates on start).
Expected: `frontend/bindings/.../services` now exports `GetUpdateBranchDetail` and an `UpdateDetail` type. Verify:

```bash
grep -rl "GetUpdateBranchDetail\|UpdateDetail" frontend/bindings/ | head
```

- [ ] **Step 2: Add detail state + lazy loader to UpdatesTab**

Add below the existing state hooks (after line 42):

```tsx
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [details, setDetails] = useState<Record<string, UpdateDetail>>({})
  const [detailLoading, setDetailLoading] = useState<string | null>(null)

  const toggleExpand = (shortName: string) => {
    setExpanded(prev => {
      const next = new Set(prev)
      if (next.has(shortName)) { next.delete(shortName); return next }
      next.add(shortName)
      if (!details[shortName]) {
        setDetailLoading(shortName)
        Services.GitService.GetUpdateBranchDetail(projectId, shortName)
          .then(d => setDetails(prev => ({ ...prev, [shortName]: d })))
          .catch(e => setError(String(e)))
          .finally(() => setDetailLoading(null))
      }
      return next
    })
  }
```

Add the import for the type (line 3 area):

```tsx
import type { UpdateBranch, UpdateDetail } from '../../bindings/github.com/rdm/sites-tool/internal/services'
```

- [ ] **Step 3: Render the chevron + expandable panel**

Wrap each branch row so the info area toggles expansion, and render a detail panel when expanded. Replace the branch `.map(...)` body (lines 126-176) with a fragment that keeps the existing row and appends a panel:

```tsx
            {branches.map(branch => {
              const isActive = branch.shortName === currentBranch
              const isOpen = expanded.has(branch.shortName)
              const detail = details[branch.shortName]
              return (
                <div key={branch.shortName}>
                  <div className={`px-4 py-3 flex items-center gap-3.5 ${isActive ? 'bg-sel' : 'hover:bg-hover'} transition-colors`}>
                    <button onClick={() => toggleExpand(branch.shortName)} className="text-fg-faint hover:text-fg text-xs w-4 shrink-0" title={isOpen ? 'Inklappen' : 'Uitklappen'}>
                      {isOpen ? '▾' : '▸'}
                    </button>
                    <span className={`w-2 h-2 rounded-full shrink-0 ${isActive ? 'bg-accent' : branch.isLocal ? 'bg-green' : 'bg-fg-faint'}`} />
                    <div className="flex-1 min-w-0 cursor-pointer" onClick={() => toggleExpand(branch.shortName)}>
                      <p className={`text-[13px] font-mono truncate ${isActive ? 'text-fg font-semibold' : 'text-fg font-medium'}`}>{branch.shortName}</p>
                      <p className="text-[11.5px] font-[450] text-fg-faint mt-0.5">{formatDate(branch.dateStr)}<span className="ml-2">{timeAgo(branch.dateStr)}</span></p>
                    </div>
                    {isActive ? (
                      <span className="shrink-0 text-[11.5px] font-semibold text-accent bg-accent-soft px-3 py-[5px] rounded-[7px]">● actief</span>
                    ) : branch.isLocal ? (
                      <button onClick={() => checkout(branch)} disabled={checkingOut !== null} className="shrink-0 px-3 py-[5px] text-[11.5px] font-semibold text-green bg-green-soft rounded-[7px] hover:brightness-95 transition-colors disabled:opacity-50 flex items-center gap-1.5">
                        {checkingOut === branch.shortName ? <><span className="animate-spin inline-block text-xs">↻</span> Schakelen…</> : '⇄ Schakel'}
                      </button>
                    ) : (
                      <button onClick={() => checkout(branch)} disabled={checkingOut !== null} className="shrink-0 px-3 py-[5px] text-[11.5px] font-semibold text-accent bg-accent-soft rounded-[7px] hover:brightness-95 transition-colors disabled:opacity-50 flex items-center gap-1.5">
                        {checkingOut === branch.shortName ? <><span className="animate-spin inline-block text-xs">↻</span> Checken…</> : '↓ Checkout'}
                      </button>
                    )}
                  </div>
                  {isOpen && (
                    <div className="px-4 pb-4 pl-[38px] bg-panel-2/40">
                      {detailLoading === branch.shortName && !detail ? (
                        <div className="text-fg-faint text-xs py-2"><span className="animate-spin inline-block">↻</span> Laden…</div>
                      ) : detail ? (
                        <UpdateDetailPanel detail={detail} />
                      ) : null}
                    </div>
                  )}
                </div>
              )
            })}
```

- [ ] **Step 4: Add the UpdateDetailPanel component**

Add above `export default function UpdatesTab` (after `timeAgo`, ~line 37):

```tsx
function Section({ title, count, children }: { title: string; count: number; children: React.ReactNode }) {
  if (count === 0) return null
  return (
    <div className="mt-3">
      <p className="text-[11px] font-semibold uppercase tracking-wide text-fg-faint mb-1.5">{title} <span className="text-fg-muted">({count})</span></p>
      <div className="flex flex-col gap-1">{children}</div>
    </div>
  )
}

function Row({ label, badge, badgeClass }: { label: string; badge?: string; badgeClass?: string }) {
  return (
    <div className="flex items-center gap-2 text-[12px] font-mono text-fg">
      <span className="truncate">{label}</span>
      {badge && <span className={`text-[10px] px-1.5 py-0.5 rounded ${badgeClass ?? 'bg-panel-2 text-fg-muted'}`}>{badge}</span>}
    </div>
  )
}

function UpdateDetailPanel({ detail }: { detail: UpdateDetail }) {
  const empty = detail.wpCore.length + detail.wpPlugins.length + detail.wpThemes.length + detail.npmApplied.length + detail.npmAvailableMajors.length === 0
  if (empty) return <p className="text-fg-faint text-xs italic py-2">Geen update-details in deze branch.</p>
  return (
    <div>
      <Section title="WordPress core" count={detail.wpCore.length}>
        {detail.wpCore.map(c => <Row key={c.version} label={c.version} badge={c.updateType} badgeClass={c.updateType === 'major' ? 'bg-amber-soft text-amber' : 'bg-green-soft text-green'} />)}
      </Section>
      <Section title="Plugins" count={detail.wpPlugins.length}>
        {detail.wpPlugins.map(p => <Row key={p.name} label={`${p.name}  ${p.from} → ${p.to}`} />)}
      </Section>
      <Section title="Thema's" count={detail.wpThemes.length}>
        {detail.wpThemes.map(p => <Row key={p.name} label={`${p.name}  ${p.from} → ${p.to}`} />)}
      </Section>
      <Section title="NPM — uitgevoerd" count={detail.npmApplied.length}>
        {detail.npmApplied.map(p => <Row key={p.name} label={`${p.name}  ${p.from} → ${p.to}`} badge={p.type} />)}
      </Section>
      <Section title="⚠️ NPM majors — niet uitgevoerd (meerwerk)" count={detail.npmAvailableMajors.length}>
        {detail.npmAvailableMajors.map(p => <Row key={p.name} label={`${p.name}  ${p.from} → ${p.to}`} badge="major" badgeClass="bg-amber-soft text-amber" />)}
      </Section>
      {detail.source === 'fallback' && detail.npmAvailableMajors.length === 0 && (
        <p className="text-[11px] text-fg-faint italic mt-3">Majors-info niet beschikbaar voor deze (oudere) branch.</p>
      )}
    </div>
  )
}
```

- [ ] **Step 5: Verify in the running dev app**

The dev server auto-reloads. In the app: open a project with update branches → open the **Updates** tab → click a branch row's chevron. Confirm:
- The `automated/updates-2026-07-20...` branch expands and shows the WordPress section (fallback), with the "majors-info niet beschikbaar" note.
- No console errors (`preview_logs` / browser devtools).

Take a screenshot to confirm the layout renders.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/UpdatesTab.tsx frontend/bindings
git commit -m "feat(tool): expandable update-branch rows showing all updates"
```

---

## Self-Review notes

- **Spec coverage:** Manifest (Task 1-3), workflow/PR majors section (Task 4), Go manifest+fallback (Task 5-6), tool expand UI (Task 7). All spec sections covered.
- **Type consistency:** `UpdateDetail`/`PackageUpdate`/`WPCoreUpdate` JSON tags (`wpCore`, `wpPlugins`, `npmApplied`, `npmAvailableMajors`, `source`) match the frontend field access in Task 7. Manifest field names (`wordpress.core`, `npm.applied`, `npm.availableMajors`) match `manifestFile` tags in Task 5 and the Node `buildManifest` output in Task 3.
- **Fallback limitation:** availableMajors intentionally empty for fallback branches (documented in spec + surfaced in UI).
