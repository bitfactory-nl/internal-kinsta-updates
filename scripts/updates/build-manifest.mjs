#!/usr/bin/env node
'use strict'
// Zelfstandig script: bouwt .updates.json (manifest v2) uit /tmp-inputs.
// Canoniek bestand — check-updates.yml embed exact dezelfde inhoud via heredoc;
// de drift-test in manifest.test.js bewaakt dit.

import fs from 'node:fs'
import path from 'node:path'
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
  fs.mkdirSync(path.dirname(outFile), { recursive: true })
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
    outFile: '.rdm/updates.json',
  })
}
