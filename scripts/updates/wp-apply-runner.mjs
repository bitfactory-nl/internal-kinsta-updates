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
