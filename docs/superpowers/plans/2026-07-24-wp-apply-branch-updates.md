# WP-updates uitvoeren in de branch + handmatige acties in de tool — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** De wekelijkse check voert WP core/gratis-.org-plugin/thema-updates daadwerkelijk uit in de update-branch (bestanden gedownload + gecommit); wat niet veilig kan krijgt status `manual` en is prominent zichtbaar in PR-body én de Updates-tab van de tool.

**Architecture:** Twee zelfstandige ESM-scripts (`wp-apply-runner.mjs` doet de updates in de werkboom; `build-manifest.mjs` schrijft `.updates.json` v2) worden als heredoc in de reusable workflow embed (drift-test bewaakt sync). De workflow commit voortaan via echte git (blob-API kan geen duizenden core-bestanden aan); github-script doet alleen nog PR-body + cleanup. Go-backend krijgt `Status`/`Reason`-velden; de tool toont handmatige acties bovenaan + als amber badge op de branch-rij.

**Tech Stack:** GitHub Actions (bash + `actions/github-script@v7`), Node 24 ESM (`node:test`), Go 1.25 (`gitcli`-adapter, table-driven tests), React + TS + Wails-bindings.

## Global Constraints

- **Branch-only**: geen enkele server-side mutatie; de SSH-stap blijft read-only.
- **.org-bewijs** voor plugins/thema's: slug matcht `^[a-z0-9][a-z0-9-]*$` ÉN .org-API kent de slug ÉN .org-versie === `update_version` ÉN pakket is in git getrackt. Anders `manual`.
- **Reason-vocabulaire** (exact): `premium`, `niet in git`, `download mislukt`, `unzip ontbreekt`, `core niet in git`, `ongeldige slug`.
- Downloads uitsluitend van `https://downloads.wordpress.org/` (URL-validatie vóór elke download); slugs gesaneerd vóór pad-gebruik.
- Core → hoogste aangeboden versie (incl. major); `wp-content/` nooit aanraken; npm blijft minor/patch-only (ongewijzigd).
- Per-item fouten → `manual` + reden; nooit de hele run laten falen op één pakket.
- `/tmp`-scripts zijn self-contained ESM (geen `require` van repo-bestanden mogelijk — workspace = klant-repo); embedded copies exact gelijk aan de canonieke bestanden (drift-test, whole-file genormaliseerd).
- Heredoc-inhoud in YAML op exact de basis-indentatie van het `run:`-blok; terminators (`WP_APPLY_RUNNER`, `BUILD_MANIFEST`) ook.
- Node-tests draaien per bestand: `node --test scripts/updates/<file>` (directory-vorm werkt hier niet).
- Go: errors wrappen met `fmt.Errorf("...: %w", err)`; table-driven tests.

---

## Phase 1 — Node: runner + manifest-builder (unit-getest)

### Task 1: `wp-apply-runner.mjs` — pure beslislogica + tests

**Files:**
- Create: `scripts/updates/wp-apply-runner.mjs`
- Test: `scripts/updates/wp-apply.test.mjs`

**Interfaces:**
- Produces (ESM named exports): `sections`, `dataRows`, `parseWpUpdates(stdout)` (core-rijen mét `packageUrl`), `sanitizeSlug(name)`, `compareVersions(a,b)`, `pickCoreUpdate(rows)`, `validateDownloadUrl(url)`, `verifyOrgPackage({slug,updateVersion,apiResponse}) => {ok,url?}|{ok:false,reason}` en `main({repoDir,inputFile,outFile})` (Task 2).

- [ ] **Step 1: Write the failing test**

```js
// scripts/updates/wp-apply.test.mjs
import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  parseWpUpdates, sanitizeSlug, compareVersions, pickCoreUpdate,
  validateDownloadUrl, verifyOrgPackage,
} from './wp-apply-runner.mjs'

test('parseWpUpdates bewaart de core package_url', () => {
  const out = '=== WORDPRESS CORE ===\nversion\tupdate_type\tpackage_url\n7.0.2\tmajor\thttps://downloads.wordpress.org/release/nl_NL/wordpress-7.0.2.zip\n=== PLUGINS ===\nname\tversion\tupdate_version\nsvg-support\t2.5.14\t2.5.17\n=== THEMES ===\n'
  const r = parseWpUpdates(out)
  assert.equal(r.core[0].packageUrl, 'https://downloads.wordpress.org/release/nl_NL/wordpress-7.0.2.zip')
  assert.deepEqual(r.plugins, [{ name: 'svg-support', from: '2.5.14', to: '2.5.17' }])
})

test('sanitizeSlug weigert vreemde tekens', () => {
  assert.ok(sanitizeSlug('wp-defender'))
  assert.ok(!sanitizeSlug('../evil'))
  assert.ok(!sanitizeSlug('Slug With Spaces'))
  assert.ok(!sanitizeSlug(''))
})

test('pickCoreUpdate kiest de hoogste versie (major boven minor)', () => {
  assert.equal(compareVersions('7.0.2', '6.9.5') > 0, true)
  const pick = pickCoreUpdate([
    { version: '6.9.5', updateType: 'minor', packageUrl: 'https://downloads.wordpress.org/a.zip' },
    { version: '7.0.2', updateType: 'major', packageUrl: 'https://downloads.wordpress.org/b.zip' },
  ])
  assert.equal(pick.version, '7.0.2')
  assert.equal(pickCoreUpdate([]), null)
})

test('validateDownloadUrl accepteert alleen downloads.wordpress.org', () => {
  assert.ok(validateDownloadUrl('https://downloads.wordpress.org/plugin/x.zip'))
  assert.ok(!validateDownloadUrl('https://evil.example/x.zip'))
  assert.ok(!validateDownloadUrl('http://downloads.wordpress.org/x.zip'))
})

test('verifyOrgPackage: alleen exacte .org-versiematch is ok', () => {
  const api = { version: '2.5.17', download_link: 'https://downloads.wordpress.org/plugin/svg-support.2.5.17.zip' }
  assert.deepEqual(verifyOrgPackage({ slug: 'svg-support', updateVersion: '2.5.17', apiResponse: api }),
    { ok: true, url: api.download_link })
  assert.deepEqual(verifyOrgPackage({ slug: 'gravityforms', updateVersion: '2.10.5', apiResponse: { error: 'Plugin not found.' } }),
    { ok: false, reason: 'premium' })
  assert.deepEqual(verifyOrgPackage({ slug: 'svg-support', updateVersion: '2.5.17', apiResponse: { version: '2.5.16', download_link: api.download_link } }),
    { ok: false, reason: 'premium' })
  assert.deepEqual(verifyOrgPackage({ slug: 'x', updateVersion: '1', apiResponse: null }),
    { ok: false, reason: 'premium' })
  assert.deepEqual(verifyOrgPackage({ slug: '../evil', updateVersion: '1', apiResponse: api }),
    { ok: false, reason: 'ongeldige slug' })
  assert.deepEqual(verifyOrgPackage({ slug: 'x', updateVersion: '1.0', apiResponse: { version: '1.0', download_link: 'https://evil.example/x.zip' } }),
    { ok: false, reason: 'download mislukt' })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `node --test scripts/updates/wp-apply.test.mjs`
Expected: FAIL — `Cannot find module ... wp-apply-runner.mjs`

- [ ] **Step 3: Write the pure functions**

```js
// scripts/updates/wp-apply-runner.mjs
#!/usr/bin/env node
'use strict'
// Zelfstandige runner: voert WordPress-updates uit in de werkboom (branch-only,
// geen server-side effecten). Canoniek bestand — check-updates.yml embed exact
// dezelfde inhoud via heredoc; de drift-test in manifest.test.js bewaakt dit.
// Bevat eigen kopieën van de parse-helpers omdat het script standalone in /tmp
// draait (de workflow-workspace bevat de klant-repo, niet deze repo).

import fs from 'node:fs'
import path from 'node:path'
import { spawnSync } from 'node:child_process'
import { pathToFileURL } from 'node:url'

export function sections(stdout) {
  const out = {}
  let current = null
  for (const rawLine of String(stdout).split('\n')) {
    const line = rawLine.replace(/\r$/, '')
    const header = line.match(/^===\s*(.+?)\s*===$/)
    if (header) {
      const name = header[1].toUpperCase()
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

export function dataRows(lines, headerFirstCol) {
  return (lines || [])
    .filter((l) => l.includes('\t'))
    .map((l) => l.split('\t'))
    .filter((cols) => cols[0] && cols[0] !== headerFirstCol)
}

export function parseWpUpdates(stdout) {
  const s = sections(stdout)
  const core = dataRows(s['WORDPRESS CORE'], 'version').map((c) => ({
    version: c[0],
    updateType: c[1] || '',
    packageUrl: c[2] || '',
  }))
  const toPkg = (rows) => rows.map((c) => ({ name: c[0], from: c[1] || '', to: c[2] || '' }))
  return {
    core,
    plugins: toPkg(dataRows(s['PLUGINS'], 'name')),
    themes: toPkg(dataRows(s['THEMES'], 'name')),
  }
}

export function sanitizeSlug(name) {
  return /^[a-z0-9][a-z0-9-]*$/.test(String(name))
}

export function compareVersions(a, b) {
  const pa = String(a).split('.').map((n) => parseInt(n, 10) || 0)
  const pb = String(b).split('.').map((n) => parseInt(n, 10) || 0)
  const len = Math.max(pa.length, pb.length)
  for (let i = 0; i < len; i++) {
    const d = (pa[i] || 0) - (pb[i] || 0)
    if (d !== 0) return d < 0 ? -1 : 1
  }
  return 0
}

export function pickCoreUpdate(rows) {
  let best = null
  for (const row of rows || []) {
    if (!row || !row.version) continue
    if (!best || compareVersions(row.version, best.version) > 0) best = row
  }
  return best
}

export function validateDownloadUrl(url) {
  return /^https:\/\/downloads\.wordpress\.org\//.test(String(url))
}

export function verifyOrgPackage({ slug, updateVersion, apiResponse }) {
  if (!sanitizeSlug(slug)) return { ok: false, reason: 'ongeldige slug' }
  if (!apiResponse || apiResponse.error || !apiResponse.version) {
    return { ok: false, reason: 'premium' }
  }
  if (String(apiResponse.version) !== String(updateVersion)) {
    return { ok: false, reason: 'premium' }
  }
  if (!validateDownloadUrl(apiResponse.download_link)) {
    return { ok: false, reason: 'download mislukt' }
  }
  return { ok: true, url: apiResponse.download_link }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `node --test scripts/updates/wp-apply.test.mjs`
Expected: PASS (5 tests)

- [ ] **Step 5: Commit**

```bash
git add scripts/updates/wp-apply-runner.mjs scripts/updates/wp-apply.test.mjs
git commit -m "feat(updates): wp-apply beslislogica (org-verificatie, core-keuze, sanitering)"
```

---

### Task 2: runner-glue (download/unzip/fs/git) + `main`

**Files:**
- Modify: `scripts/updates/wp-apply-runner.mjs` (append)

**Interfaces:**
- Consumes: pure functies uit Task 1.
- Produces: `main({repoDir, inputFile, outFile})` — leest WP-CLI-stdout, muteert de werkboom, schrijft `outFile` = `{core:[{version,updateType,status?,reason?}], plugins:[{name,from,to,status,reason?}], themes:[...]}`. CLI-entry via env `WP_APPLY_REPO`/`WP_APPLY_INPUT`/`WP_APPLY_OUT`.
- Glue is bewust dun; correctheid wordt in Task 7 (lokale E2E) bewezen.

- [ ] **Step 1: Append glue + main**

```js
// append aan scripts/updates/wp-apply-runner.mjs

function sh(cmd, args, cwd) {
  return spawnSync(cmd, args, { cwd, encoding: 'utf8' })
}

function firstTracked(repoDir, pathspec) {
  const r = sh('git', ['ls-files', '--', pathspec], repoDir)
  return (r.stdout || '').split('\n').filter(Boolean)[0] || ''
}

function isTracked(repoDir, relPath) {
  const r = sh('git', ['ls-files', '--', relPath], repoDir)
  return (r.stdout || '').trim() !== ''
}

async function downloadZip(url, dest) {
  const res = await fetch(url, { redirect: 'follow' })
  if (!res.ok) throw new Error(`download ${url}: HTTP ${res.status}`)
  fs.writeFileSync(dest, Buffer.from(await res.arrayBuffer()))
}

function unzipTo(zip, dir) {
  fs.rmSync(dir, { recursive: true, force: true })
  fs.mkdirSync(dir, { recursive: true })
  const r = sh('unzip', ['-q', zip, '-d', dir])
  if (r.status !== 0) throw new Error(`unzip: ${(r.stderr || r.stdout || '').trim()}`)
}

async function fetchOrgInfo(kind, slug) {
  const action = kind === 'plugins' ? 'plugin_information' : 'theme_information'
  const url = `https://api.wordpress.org/${kind}/info/1.2/?action=${action}&request[slug]=${encodeURIComponent(slug)}`
  try {
    const res = await fetch(url)
    if (!res.ok) return null
    return await res.json()
  } catch {
    return null
  }
}

async function applyCore(repoDir, wpRoot, row, haveUnzip) {
  if (!wpRoot) return { status: 'manual', reason: 'core niet in git' }
  if (!validateDownloadUrl(row.packageUrl)) return { status: 'manual', reason: 'download mislukt' }
  if (!haveUnzip) return { status: 'manual', reason: 'unzip ontbreekt' }
  try {
    const zip = '/tmp/wp-core.zip'
    const dir = '/tmp/wp-core'
    await downloadZip(row.packageUrl, zip)
    unzipTo(zip, dir)
    const src = path.join(dir, 'wordpress')
    if (!fs.existsSync(src)) return { status: 'manual', reason: 'download mislukt' }
    for (const name of ['wp-admin', 'wp-includes']) {
      fs.rmSync(path.join(repoDir, wpRoot, name), { recursive: true, force: true })
      fs.cpSync(path.join(src, name), path.join(repoDir, wpRoot, name), { recursive: true })
    }
    // Overige root-bestanden (index.php, wp-*.php, license.txt, readme.html, …).
    // wp-content is een directory en wordt dus nooit aangeraakt.
    for (const entry of fs.readdirSync(src, { withFileTypes: true })) {
      if (entry.isDirectory()) continue
      fs.copyFileSync(path.join(src, entry.name), path.join(repoDir, wpRoot, entry.name))
    }
    return { status: 'applied' }
  } catch (e) {
    console.error(`core: ${e.message}`)
    return { status: 'manual', reason: 'download mislukt' }
  }
}

async function applyPackage(repoDir, kind, baseDir, item, haveUnzip) {
  const slug = item.name
  if (!sanitizeSlug(slug)) return { ...item, status: 'manual', reason: 'ongeldige slug' }
  if (!baseDir) return { ...item, status: 'manual', reason: 'niet in git' }
  const target = path.posix.join(baseDir, slug)
  if (!isTracked(repoDir, target)) return { ...item, status: 'manual', reason: 'niet in git' }
  const info = await fetchOrgInfo(kind, slug)
  const v = verifyOrgPackage({ slug, updateVersion: item.to, apiResponse: info })
  if (!v.ok) return { ...item, status: 'manual', reason: v.reason }
  if (!haveUnzip) return { ...item, status: 'manual', reason: 'unzip ontbreekt' }
  try {
    const zip = `/tmp/pkg-${slug}.zip`
    const dir = `/tmp/pkg-${slug}`
    await downloadZip(v.url, zip)
    unzipTo(zip, dir)
    const src = path.join(dir, slug)
    if (!fs.existsSync(src)) return { ...item, status: 'manual', reason: 'download mislukt' }
    fs.rmSync(path.join(repoDir, target), { recursive: true, force: true })
    fs.cpSync(src, path.join(repoDir, target), { recursive: true })
    return { ...item, status: 'applied' }
  } catch (e) {
    console.error(`${slug}: ${e.message}`)
    return { ...item, status: 'manual', reason: 'download mislukt' }
  }
}

export async function main({ repoDir, inputFile, outFile }) {
  const raw = fs.readFileSync(inputFile, 'utf8')
  const parsed = parseWpUpdates(raw)
  const haveUnzip = sh('unzip', ['-v']).status === 0
  const verFile = firstTracked(repoDir, '*wp-includes/version.php')
  const wpRoot = verFile ? verFile.replace(/wp-includes\/version\.php$/, '') : ''
  const probe = firstTracked(repoDir, '*wp-content/plugins/*')
  const contentBase = probe ? probe.slice(0, probe.indexOf('wp-content/')) : wpRoot

  const result = { core: [], plugins: [], themes: [] }
  const pick = pickCoreUpdate(parsed.core)
  for (const row of parsed.core) {
    const entry = { version: row.version, updateType: row.updateType }
    if (pick && row.version === pick.version) {
      Object.assign(entry, await applyCore(repoDir, wpRoot, row, haveUnzip))
    }
    result.core.push(entry)
  }
  for (const p of parsed.plugins) {
    result.plugins.push(await applyPackage(repoDir, 'plugins', contentBase ? contentBase + 'wp-content/plugins' : '', p, haveUnzip))
  }
  for (const t of parsed.themes) {
    result.themes.push(await applyPackage(repoDir, 'themes', contentBase ? contentBase + 'wp-content/themes' : '', t, haveUnzip))
  }

  fs.writeFileSync(outFile, JSON.stringify(result, null, 2) + '\n')
  const all = [...result.core, ...result.plugins, ...result.themes]
  const applied = all.filter((x) => x.status === 'applied').length
  const manual = all.filter((x) => x.status === 'manual').length
  console.log(`WP-apply: ${applied} uitgevoerd, ${manual} handmatig — details in ${outFile}`)
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main({
    repoDir: process.env.WP_APPLY_REPO || process.cwd(),
    inputFile: process.env.WP_APPLY_INPUT || '/tmp/wp-updates.txt',
    outFile: process.env.WP_APPLY_OUT || '/tmp/wp-applied.json',
  }).catch((e) => {
    console.error(e)
    process.exit(1)
  })
}
```

- [ ] **Step 2: Tests + syntax check**

Run: `node --test scripts/updates/wp-apply.test.mjs && node --check scripts/updates/wp-apply-runner.mjs`
Expected: PASS (5 tests), geen syntaxfout. (Import van het testbestand voert `main` níét uit dankzij de argv-guard.)

- [ ] **Step 3: Commit**

```bash
git add scripts/updates/wp-apply-runner.mjs
git commit -m "feat(updates): wp-apply runner-glue (download/unzip/werkboom) + main"
```

---

### Task 3: `build-manifest.mjs` + verplaatsing uit `manifest.js` + nieuwe PR-renderers

**Files:**
- Create: `scripts/updates/build-manifest.mjs`
- Create: `scripts/updates/build-manifest.test.mjs`
- Modify: `scripts/updates/manifest.js` (functies verplaatsen/toevoegen)
- Modify: `scripts/updates/manifest.test.js` (tests mee verplaatsen/aanpassen)

**Interfaces:**
- Consumes: `/tmp/wp-applied.json` (Task 2-vorm), `/tmp/npm-{current,minor,latest}.json`.
- Produces:
  - `build-manifest.mjs` (ESM exports): `parseSemver`, `classifyBump`, `computeNpmUpdates`, `buildManifest`, `main({generatedAt, wpAppliedFile, npmCurrentFile, npmMinorFile, npmLatestFile, outFile})` → schrijft `.updates.json` v2.
  - `manifest.js` (CJS, alléén nog PR-renderers voor github-script): `renderNpmMajorsSection(availableMajors)`, `renderManualSection(items)`, `renderWpAppliedSection(wordpress)`, `collectManualWpItems(wordpress)`.
  - `parseWpUpdates`/`sections`/`dataRows` verhuizen conceptueel naar de runner (Task 1) en verdwijnen uit `manifest.js`.

- [ ] **Step 1: Write failing tests**

```js
// scripts/updates/build-manifest.test.mjs
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { classifyBump, computeNpmUpdates, buildManifest } from './build-manifest.mjs'

test('classifyBump onderscheidt major/minor/patch met range-prefixen', () => {
  assert.equal(classifyBump('^1.99.0', '^1.101.7'), 'minor')
  assert.equal(classifyBump('10.5.0', '10.5.4'), 'patch')
  assert.equal(classifyBump('9.39.2', '10.7.0'), 'major')
})

test('computeNpmUpdates splitst applied en availableMajors', () => {
  const r = computeNpmUpdates({
    current: { sass: '^1.99.0', eslint: '9.39.2' },
    minor: { sass: '^1.101.7' },
    latest: { sass: '^1.101.7', eslint: '10.7.0' },
  })
  assert.deepEqual(r.applied, [{ name: 'sass', from: '^1.99.0', to: '^1.101.7', type: 'minor' }])
  assert.deepEqual(r.availableMajors, [{ name: 'eslint', from: '9.39.2', to: '10.7.0' }])
})

test('buildManifest laat status/reason op WP-items ongemoeid (v2 passthrough)', () => {
  const m = buildManifest({
    generatedAt: 'x',
    wordpress: {
      core: [{ version: '7.0.2', updateType: 'major', status: 'applied' }],
      plugins: [{ name: 'gravityforms', from: '2.10.2', to: '2.10.5', status: 'manual', reason: 'premium' }],
      themes: [],
    },
    npm: { applied: [], availableMajors: [] },
  })
  assert.equal(m.wordpress.core[0].status, 'applied')
  assert.equal(m.wordpress.plugins[0].reason, 'premium')
})
```

En in `scripts/updates/manifest.test.js`: verwijder de tests voor `parseWpUpdates`, `classifyBump`, `computeNpmUpdates`, `buildManifest` (verhuisd) en voeg toe:

```js
const { renderManualSection, renderWpAppliedSection, collectManualWpItems } = require('./manifest')

test('renderManualSection toont premium-items met reden, of leeg', () => {
  assert.equal(renderManualSection([]), '')
  const s = renderManualSection([{ name: 'gravityforms', from: '2.10.2', to: '2.10.5', reason: 'premium' }])
  assert.match(s, /Handmatig bijwerken/)
  assert.match(s, /gravityforms.*2\.10\.2.*2\.10\.5.*premium/)
})

test('renderWpAppliedSection + collectManualWpItems splitsen op status', () => {
  const wordpress = {
    core: [{ version: '7.0.2', updateType: 'major', status: 'applied' }],
    plugins: [
      { name: 'svg-support', from: '2.5.14', to: '2.5.17', status: 'applied' },
      { name: 'gravityforms', from: '2.10.2', to: '2.10.5', status: 'manual', reason: 'premium' },
    ],
    themes: [],
  }
  const applied = renderWpAppliedSection(wordpress)
  assert.match(applied, /WordPress core → 7\.0\.2/)
  assert.match(applied, /svg-support/)
  assert.ok(!applied.includes('gravityforms'))
  const manual = collectManualWpItems(wordpress)
  assert.deepEqual(manual.map((m) => m.name), ['gravityforms'])
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `node --test scripts/updates/build-manifest.test.mjs && node --test scripts/updates/manifest.test.js`
Expected: FAIL (module ontbreekt resp. functies ontbreken)

- [ ] **Step 3: Implement**

`scripts/updates/build-manifest.mjs` (volledig):

```js
#!/usr/bin/env node
'use strict'
// Zelfstandig script: bouwt .updates.json (manifest v2) uit /tmp-inputs.
// Canoniek bestand — check-updates.yml embed exact dezelfde inhoud via heredoc;
// de drift-test in manifest.test.js bewaakt dit.

import fs from 'node:fs'
import { pathToFileURL } from 'node:url'

export function parseSemver(v) {
  const m = String(v).replace(/^[^\d]*/, '').match(/^(\d+)\.(\d+)\.(\d+)/)
  if (!m) return null
  return { major: +m[1], minor: +m[2], patch: +m[3] }
}

export function classifyBump(from, to) {
  const a = parseSemver(from)
  const b = parseSemver(to)
  if (!a || !b) return 'minor'
  if (b.major !== a.major) return 'major'
  if (b.minor !== a.minor) return 'minor'
  return 'patch'
}

export function computeNpmUpdates({ current = {}, minor = {}, latest = {} }) {
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

export function buildManifest({ generatedAt, wordpress, npm }) {
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

export function main({ generatedAt, wpAppliedFile, npmCurrentFile, npmMinorFile, npmLatestFile, outFile }) {
  const readJson = (p, fallback) => {
    try {
      return JSON.parse(fs.readFileSync(p, 'utf8'))
    } catch {
      return fallback
    }
  }
  const wordpress = readJson(wpAppliedFile, { core: [], plugins: [], themes: [] })
  const npm = computeNpmUpdates({
    current: readJson(npmCurrentFile, {}),
    minor: readJson(npmMinorFile, {}),
    latest: readJson(npmLatestFile, {}),
  })
  const manifest = buildManifest({ generatedAt, wordpress, npm })
  fs.writeFileSync(outFile, JSON.stringify(manifest, null, 2) + '\n')
  console.log(`Manifest geschreven naar ${outFile}`)
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main({
    generatedAt: process.env.MANIFEST_DATE || new Date().toISOString(),
    wpAppliedFile: '/tmp/wp-applied.json',
    npmCurrentFile: '/tmp/npm-current.json',
    npmMinorFile: '/tmp/npm-minor.json',
    npmLatestFile: '/tmp/npm-latest.json',
    outFile: '.updates.json',
  })
}
```

`scripts/updates/manifest.js` wordt (volledig, ter vervanging):

```js
'use strict'

// PR-body-renderers. LET OP: de github-script-stap in
// .github/workflows/check-updates.yml bevat een embedded copy van deze
// functies (een reusable workflow kan dit bestand niet require'n). De
// drift-test in manifest.test.js dwingt af dat beide gelijk blijven.
// De parse/compute-logica leeft in wp-apply-runner.mjs en build-manifest.mjs.

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

function renderManualSection(items) {
  if (!items || items.length === 0) return ''
  const lines = items
    .map((i) => `- \`${i.name}\` ${i.from ? i.from + ' → ' : ''}${i.to} — ${i.reason || 'handmatig'}`)
    .join('\n')
  return [
    '### ⚠️ Handmatig bijwerken — niet automatisch uitgevoerd',
    '',
    'Deze updates konden niet veilig automatisch worden toegepast (premium/licentie of niet via wordpress.org verifieerbaar). Voer ze later handmatig uit:',
    '',
    lines,
    '',
  ].join('\n')
}

function renderWpAppliedSection(wordpress) {
  const items = []
  for (const c of (wordpress && wordpress.core) || []) {
    if (c.status === 'applied') items.push(`- WordPress core → ${c.version} (${c.updateType})`)
  }
  for (const p of (wordpress && wordpress.plugins) || []) {
    if (p.status === 'applied') items.push(`- \`${p.name}\` ${p.from} → ${p.to}`)
  }
  for (const t of (wordpress && wordpress.themes) || []) {
    if (t.status === 'applied') items.push(`- thema \`${t.name}\` ${t.from} → ${t.to}`)
  }
  if (items.length === 0) return ''
  return [
    '### WordPress updates — uitgevoerd in deze branch',
    '',
    'De bestanden zijn bijgewerkt en zitten in de diff van deze PR. Na deploy kunnen database-migraties nodig zijn.',
    '',
    items.join('\n'),
    '',
  ].join('\n')
}

function collectManualWpItems(wordpress) {
  const out = []
  for (const c of (wordpress && wordpress.core) || []) {
    if (c.status === 'manual') out.push({ name: 'WordPress core', from: '', to: c.version, reason: c.reason })
  }
  for (const p of (wordpress && wordpress.plugins) || []) {
    if (p.status === 'manual') out.push(p)
  }
  for (const t of (wordpress && wordpress.themes) || []) {
    if (t.status === 'manual') out.push(t)
  }
  return out
}

module.exports = { renderNpmMajorsSection, renderManualSection, renderWpAppliedSection, collectManualWpItems }
```

`manifest.test.js`: verwijder de imports/tests van verplaatste functies; de drift-test wordt in Task 4 herschreven (laat hem in deze task tijdelijk weg — verwijder de oude drift-test hier, hij komt in Task 4 terug in nieuwe vorm).

- [ ] **Step 4: Run tests to verify they pass**

Run: `node --test scripts/updates/build-manifest.test.mjs && node --test scripts/updates/manifest.test.js && node --test scripts/updates/wp-apply.test.mjs`
Expected: PASS overal

- [ ] **Step 5: Commit**

```bash
git add scripts/updates/
git commit -m "feat(updates): manifest v2 builder + PR-renderers voor applied/manual"
```

---

## Phase 2 — Workflow

### Task 4: check-updates.yml herschrijven (apply-stap, git-commit, PR v2, drift-test)

**Files:**
- Modify: `.github/workflows/check-updates.yml`
- Modify: `scripts/updates/manifest.test.js` (nieuwe drift-test)

**Interfaces:**
- Consumes: `scripts/updates/wp-apply-runner.mjs` + `build-manifest.mjs` (embedded whole-file), `manifest.js`-renderers (embedded function-level).
- Produces: workflow-stappen `wp_apply`, `make_branch` (output `branch`), aangepaste PR-stap. `.updates.json` + `.wp-update-log` als echte bestanden in de branch-commit.

- [ ] **Step 1: Setup Node onvoorwaardelijk maken**

Verwijder de `if: hashFiles('package.json') != ''` op de "Setup Node"-stap in de `check-updates`-job (de WP-apply-stap heeft Node ook nodig; de conditie op de npm-stap zelf blijft).

- [ ] **Step 2: wp_apply-stap toevoegen (direct na "Controleer NPM updates")**

```yaml
      - name: Voer WordPress updates uit (branch-only)
        id: wp_apply
        if: steps.check_updates.outputs.has_updates == 'true'
        shell: bash
        env:
          SSH_STDOUT: ${{ steps.get_updates.outputs.stdout }}
        run: |
          set -euo pipefail
          printf '%s' "$SSH_STDOUT" > /tmp/wp-updates.txt
          cat > /tmp/wp-apply-runner.mjs <<'WP_APPLY_RUNNER'
          <EXACTE inhoud van scripts/updates/wp-apply-runner.mjs, op basis-indentatie>
          WP_APPLY_RUNNER
          node /tmp/wp-apply-runner.mjs
```

- [ ] **Step 3: make_branch-stap (vervangt de blob/tree-machinerie)**

```yaml
      - name: Maak update branch (git)
        id: make_branch
        if: steps.check_updates.outputs.has_updates == 'true' || steps.check_npm.outputs.has_npm_updates == 'true'
        shell: bash
        env:
          SSH_STDOUT: ${{ steps.get_updates.outputs.stdout }}
          HAS_WP: ${{ steps.check_updates.outputs.has_updates }}
          HAS_NPM: ${{ steps.check_npm.outputs.has_npm_updates }}
        run: |
          set -euo pipefail
          DATE=$(date -u +%Y-%m-%dT%H-%M-%S)
          BRANCH="automated/updates-${DATE}"
          if [ "$HAS_WP" = "true" ]; then
            {
              echo "WordPress update check uitgevoerd op: ${DATE}"
              echo
              printf '%s\n' "$SSH_STDOUT"
              echo
              echo "Zie .updates.json voor de status per update (applied/manual)."
            } > .wp-update-log
          fi
          cat > /tmp/build-manifest.mjs <<'BUILD_MANIFEST'
          <EXACTE inhoud van scripts/updates/build-manifest.mjs, op basis-indentatie>
          BUILD_MANIFEST
          MANIFEST_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)" node /tmp/build-manifest.mjs
          git config user.name "github-actions[bot]"
          git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
          git checkout -b "$BRANCH"
          git add -A
          if git diff --cached --quiet; then
            echo "Geen wijzigingen om te committen — geen branch/PR."
            exit 0
          fi
          if [ "$HAS_WP" = "true" ] && [ "$HAS_NPM" = "true" ]; then
            MSG="chore: WordPress + NPM updates ${DATE}"
          elif [ "$HAS_WP" = "true" ]; then
            MSG="chore: WordPress updates ${DATE}"
          else
            MSG="chore: NPM minor/patch updates ${DATE}"
          fi
          git commit -m "$MSG"
          git push origin "$BRANCH"
          echo "branch=$BRANCH" >> "$GITHUB_OUTPUT"
```

- [ ] **Step 4: PR-stap vervangen**

Vervang de volledige `Maak update branch en PR aan`-stap door:

```yaml
      - name: Maak PR aan en ruim oude branches op
        if: steps.make_branch.outputs.branch != ''
        uses: actions/github-script@v7
        env:
          BRANCH: ${{ steps.make_branch.outputs.branch }}
          UPDATE_OUTPUT: ${{ steps.get_updates.outputs.stdout }}
          HAS_WP: ${{ steps.check_updates.outputs.has_updates }}
          HAS_NPM: ${{ steps.check_npm.outputs.has_npm_updates }}
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          script: |
            const fs = require('fs');
            // === BEGIN manifest render helpers ===
            // Embedded copy van scripts/updates/manifest.js — drift-test bewaakt sync.
            <EXACTE kopie van de vier render/collect-functies uit manifest.js>
            // === EINDE manifest render helpers ===
            const branch = process.env.BRANCH;
            const hasWp = process.env.HAS_WP === 'true';
            const hasNpm = process.env.HAS_NPM === 'true';
            const updateOutput = process.env.UPDATE_OUTPUT || '';
            const manifest = JSON.parse(fs.readFileSync('.updates.json', 'utf8'));
            const defaultBranch = context.payload.repository.default_branch;
            const date = branch.replace('automated/updates-', '');

            let body = `## Automatische update check\n\nGegenereerd op ${date}.\n\n`;
            body += renderWpAppliedSection(manifest.wordpress);
            body += renderManualSection(collectManualWpItems(manifest.wordpress));
            if (hasWp) {
              body += `<details><summary>Ruwe WP-CLI output</summary>\n\n\`\`\`\n${updateOutput}\n\`\`\`\n\n</details>\n\n`;
            }
            if (hasNpm && manifest.npm.applied.length) {
              const lines = manifest.npm.applied.map((u) => `- \`${u.name}\` ${u.from} → ${u.to} (${u.type})`).join('\n');
              body += `### NPM updates (minor + patch)\n\n\`package.json\` en \`package-lock.json\` zijn bijgewerkt.\n\n${lines}\n\n`;
            }
            body += renderNpmMajorsSection(manifest.npm.availableMajors);

            const title = hasWp && hasNpm ? `Updates - ${date}` : hasWp ? `WordPress Updates - ${date}` : `NPM Updates - ${date}`;
            await github.rest.pulls.create({
              owner: context.repo.owner,
              repo: context.repo.repo,
              title,
              head: branch,
              base: defaultBranch,
              body
            });
```

…gevolgd door de bestaande cleanup-logica (oldPrefixes/listMatchingRefs/close/delete) **ongewijzigd overnemen** uit de huidige stap.

- [ ] **Step 5: Drift-test herschrijven in manifest.test.js**

```js
test('workflow embeds de canonieke scripts en renderers (drift guard)', () => {
  const fs = require('node:fs')
  const path = require('node:path')
  const norm = (s) => s.split('\n').map((l) => l.trim()).filter(Boolean).join('\n')
  const wf = norm(fs.readFileSync(path.join(__dirname, '../../.github/workflows/check-updates.yml'), 'utf8'))

  // Whole-file embeds
  for (const file of ['wp-apply-runner.mjs', 'build-manifest.mjs']) {
    const src = norm(fs.readFileSync(path.join(__dirname, file), 'utf8'))
    assert.ok(wf.includes(src), `embedded copy van ${file} wijkt af — synchroniseer check-updates.yml`)
  }

  // Function-level embeds uit manifest.js
  const src = fs.readFileSync(path.join(__dirname, 'manifest.js'), 'utf8')
  for (const name of ['renderNpmMajorsSection', 'renderManualSection', 'renderWpAppliedSection', 'collectManualWpItems']) {
    const m = src.match(new RegExp('^function ' + name + '[\\s\\S]*?^}', 'm'))
    assert.ok(m, `functie ${name} niet gevonden in manifest.js`)
    assert.ok(wf.includes(norm(m[0])), `embedded copy van ${name} wijkt af — synchroniseer check-updates.yml`)
  }
})
```

- [ ] **Step 6: Valideren**

```bash
node --test scripts/updates/manifest.test.js
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/check-updates.yml')); print('YAML OK')"
python3 - <<'PY'
import yaml
d = yaml.safe_load(open('.github/workflows/check-updates.yml'))
for s in d['jobs']['check-updates']['steps']:
    if s.get('name','').startswith('Maak PR aan'):
        open('/tmp/gs.js','w').write('async function main(github, context){\n' + s['with']['script'] + '\n}')
print('script extracted')
PY
node --check /tmp/gs.js
```
Expected: tests PASS (incl. drift), YAML OK, JS syntax OK.

- [ ] **Step 7: Commit**

```bash
git add .github/workflows/check-updates.yml scripts/updates/manifest.test.js
git commit -m "feat(workflow): voer WP-updates uit in de branch, git-based commit, PR v2 met handmatig-sectie"
```

---

## Phase 3 — Go backend

### Task 5: Status/Reason-velden + v2-parse-test

**Files:**
- Modify: `internal/services/git_service.go` (types `PackageUpdate`, `WPCoreUpdate`)
- Test: `internal/services/update_detail_test.go` (append)

**Interfaces:**
- Produces: `PackageUpdate.Status/Reason`, `WPCoreUpdate.Status/Reason` (beide `omitempty`); JSON-namen `status`/`reason`. `GetUpdateBranchDetail` en fallback blijven ongewijzigd (velden blijven leeg = onbekend).

- [ ] **Step 1: Write the failing test (append)**

```go
func TestParseUpdateManifestV2Status(t *testing.T) {
	data := []byte(`{
	  "wordpress": {
	    "core": [{"version":"7.0.2","updateType":"major","status":"applied"}],
	    "plugins": [{"name":"gravityforms","from":"2.10.2","to":"2.10.5","status":"manual","reason":"premium"}],
	    "themes": []
	  },
	  "npm": {"applied": [], "availableMajors": []}
	}`)
	d, err := parseUpdateManifest(data)
	if err != nil {
		t.Fatalf("parseUpdateManifest: %v", err)
	}
	if d.WPCore[0].Status != "applied" {
		t.Errorf("core status = %q, want applied", d.WPCore[0].Status)
	}
	if d.WPPlugins[0].Status != "manual" || d.WPPlugins[0].Reason != "premium" {
		t.Errorf("plugin = %+v, want manual/premium", d.WPPlugins[0])
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/services/ -run TestParseUpdateManifestV2Status`
Expected: FAIL — `d.WPCore[0].Status undefined`

- [ ] **Step 3: Voeg velden toe**

```go
// PackageUpdate is a single package version change (or availability).
type PackageUpdate struct {
	Name   string `json:"name"`
	From   string `json:"from"`
	To     string `json:"to"`
	Type   string `json:"type,omitempty"`   // "minor" | "patch" (npm applied)
	Status string `json:"status,omitempty"` // "applied" | "manual" | "" (onbekend)
	Reason string `json:"reason,omitempty"` // toelichting bij "manual"
}

// WPCoreUpdate is a single WordPress core version availability.
type WPCoreUpdate struct {
	Version    string `json:"version"`
	UpdateType string `json:"updateType"` // "minor" | "major"
	Status     string `json:"status,omitempty"`
	Reason     string `json:"reason,omitempty"`
}
```

- [ ] **Step 4: Run tests**

Run: `go vet ./internal/services/ && go test ./internal/services/`
Expected: vet schoon, alle tests PASS (ook de bestaande v1-test — velden zijn optioneel)

- [ ] **Step 5: Commit**

```bash
git add internal/services/git_service.go internal/services/update_detail_test.go
git commit -m "feat(tool): status/reason op update-items (manifest v2)"
```

---

## Phase 4 — Frontend

### Task 6: badge op branch-rij + handmatig-paneel bovenaan + eager loading

**Files:**
- Regenerate: Wails-bindings (`wails3 generate bindings -silent`)
- Modify: `frontend/src/components/UpdatesTab.tsx`

**Interfaces:**
- Consumes: `UpdateDetail` met `status`/`reason` op `wpCore/wpPlugins/wpThemes`-items.

- [ ] **Step 1: Bindings regenereren en veldnamen controleren**

```bash
wails3 generate bindings -silent
grep -n "status" frontend/bindings/github.com/rdm/sites-tool/internal/services/models.js | head
```
Expected: `status`/`reason` aanwezig op de gegenereerde types.

- [ ] **Step 2: Helper + eager loading in UpdatesTab**

Boven `UpdateDetailPanel` toevoegen:

```tsx
interface ManualItem { name: string; from: string; to: string; reason: string }

function collectManual(detail: UpdateDetail): ManualItem[] {
  return [
    ...detail.wpCore.filter(c => c.status === 'manual').map(c => ({ name: 'WordPress core', from: '', to: c.version, reason: c.reason ?? '' })),
    ...detail.wpPlugins.filter(p => p.status === 'manual').map(p => ({ name: p.name, from: p.from, to: p.to, reason: p.reason ?? '' })),
    ...detail.wpThemes.filter(t => t.status === 'manual').map(t => ({ name: t.name, from: t.from, to: t.to, reason: t.reason ?? '' })),
  ]
}
```

In `fetchBranches` na het zetten van de branches de details eager laden:

```tsx
  const fetchBranches = () => {
    setError(null)
    setLoading(true)
    Services.GitService.GetUpdateBranches(projectId)
      .then(b => {
        const list = b ?? []
        setBranches(list)
        return Promise.all(list.map(br =>
          Services.GitService.GetUpdateBranchDetail(projectId, br.shortName)
            .then(d => (d ? [br.shortName, d] as const : null))
            .catch(() => null)
        ))
      })
      .then(pairs => {
        if (pairs) setDetails(Object.fromEntries(pairs.filter((p): p is readonly [string, UpdateDetail] => p !== null)))
      })
      .catch(e => setError(String(e)))
      .finally(() => setLoading(false))
  }
```

(`toggleExpand` blijft bestaan als vangnet voor branches waarvan eager laden faalde.)

- [ ] **Step 3: Amber badge op de branch-rij**

In de rij, direct vóór het action-element:

```tsx
                    {(() => {
                      const d = details[branch.shortName]
                      const n = d ? collectManual(d).length : 0
                      return n > 0 ? (
                        <span className="shrink-0 text-[11px] font-semibold text-amber bg-amber-soft px-2.5 py-[4px] rounded-[7px]" title="Updates die handmatig moeten">
                          ⚠️ {n} handmatig
                        </span>
                      ) : null
                    })()}
```

- [ ] **Step 4: Handmatig-paneel bovenaan het detail + ✓-badges**

In `UpdateDetailPanel`, direct na de fallback-banner:

```tsx
      {(() => {
        const manual = collectManual(detail)
        if (manual.length === 0) return null
        return (
          <div className="mt-3 bg-amber-soft/50 border border-amber/30 rounded-lg px-3 py-2.5">
            <p className="text-[11px] font-semibold uppercase tracking-wide text-amber mb-1.5">
              ⚠️ Handmatig bijwerken <span className="opacity-70">({manual.length})</span>
            </p>
            <div className="flex flex-col gap-1">
              {manual.map(m => (
                <Row key={m.name} label={`${m.name}  ${m.from ? m.from + ' → ' : '→ '}${m.to}`} badge={m.reason} badgeClass="bg-amber-soft text-amber" />
              ))}
            </div>
          </div>
        )
      })()}
```

En de bestaande secties filteren op niet-manual + ✓ tonen bij applied:
- `WordPress core`-sectie: `detail.wpCore.filter(c => c.status !== 'manual')`, badge wordt `c.status === 'applied' ? '✓ ' + c.updateType : c.updateType`.
- `Plugins`/`Thema's`: `filter(p => p.status !== 'manual')`, badge `p.status === 'applied' ? '✓ uitgevoerd' : undefined` met `badgeClass="bg-green-soft text-green"`.
- Sectie-`count` props op de gefilterde lengtes aanpassen.

- [ ] **Step 5: Typecheck + visueel verifiëren**

```bash
cd frontend && npx tsc --noEmit
```
Expected: exit 0. Daarna in de draaiende dev-app (hot-reload): Updates-tab → badge zichtbaar op rijen met manual items (oude branches tonen géén badge — fallback heeft geen status), uitklappen toont het amber paneel bovenaan.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/UpdatesTab.tsx frontend/bindings
git commit -m "feat(tool): handmatige updates prominent (badge + amber paneel)"
```

---

## Phase 5 — Lokale E2E (zonder server, zonder merge)

### Task 7: runner end-to-end op een vanluyken-worktree

**Files:** geen wijzigingen — pure verificatie. (Werk in een **git worktree**, nooit in de gebruikers-checkout: daar staat werk-in-uitvoering.)

- [ ] **Step 1: Worktree + echte input voorbereiden**

```bash
git -C ~/Projects/web-vanluykennl fetch origin
git -C ~/Projects/web-vanluykennl worktree add /tmp/e2e-vanluyken origin/release/1.0.x
git -C ~/Projects/web-vanluykennl show origin/automated/updates-2026-07-24T10-05-28:.wp-update-log > /tmp/wp-updates.txt
```

- [ ] **Step 2: Runner draaien**

```bash
WP_APPLY_REPO=/tmp/e2e-vanluyken node /Users/jeffreyt/Projects/RDM-Sites-tool/scripts/updates/wp-apply-runner.mjs
cat /tmp/wp-applied.json
```

Expected in `/tmp/wp-applied.json`:
- core: hoogste versie `applied` (wp-admin/wp-includes vervangen), laagste rij zonder status;
- gratis .org-plugins (bv. `disable-comments`, `duracelltomi-google-tag-manager`) → `applied`;
- premium (bv. `wp-defender`, `wp-hummingbird`, `snapshot-backups`) → `manual` / `premium`.

- [ ] **Step 3: Werkboom-sanity**

```bash
git -C /tmp/e2e-vanluyken status --porcelain | awk '{print $2}' | cut -d/ -f1-4 | sort | uniq -c | sort -rn | head
git -C /tmp/e2e-vanluyken status --porcelain | grep -v -E 'wp-admin|wp-includes|wp-content/plugins|^.. [a-z-]+\.php|wp-activate|readme.html|license.txt|\.wp-update-log|\.updates\.json' | head
```
Expected: wijzigingen alléén in wp-admin/, wp-includes/, root-bestanden en de bijgewerkte plugin-mappen; tweede commando levert (vrijwel) niets.

- [ ] **Step 4: build-manifest natrekken en opruimen**

```bash
cd /tmp/e2e-vanluyken && MANIFEST_DATE=E2E node /Users/jeffreyt/Projects/RDM-Sites-tool/scripts/updates/build-manifest.mjs && cat .updates.json | head -30
git -C ~/Projects/web-vanluykennl worktree remove --force /tmp/e2e-vanluyken
```
Expected: `.updates.json` bevat wordpress-items met status + (lege) npm-arrays; worktree opgeruimd.

---

## Self-Review notes

- **Spec-dekking:** veiligheidsmodel (T1), apply branch-only (T2), workflow/git/PR v2 (T4), manifest v2 (T3), Go-status (T5), tool prominent (T6), lokale E2E (T7). Premium-POC expliciet buiten scope.
- **Type-consistentie:** reason-strings identiek in runner (T2), renderers (T3) en plan-constraints; manifestvelden `status`/`reason` matchen Go-tags (T5) en frontend-gebruik (T6). `parseWpUpdates` met `packageUrl` bestaat alleen in de runner; `manifest.js` heeft geen parse meer (github-script leest `.updates.json`).
- **Volgordes:** npm-stap draait vóór wp_apply (diff-check op package-bestanden blijft zuiver); make_branch commit alles in één commit; PR-stap alleen bij niet-lege branch-output.
