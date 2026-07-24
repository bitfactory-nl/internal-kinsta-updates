'use strict'

const { test } = require('node:test')
const assert = require('node:assert/strict')
const {
  parseWpUpdates,
  classifyBump,
  computeNpmUpdates,
  buildManifest,
  renderNpmMajorsSection,
} = require('./manifest')

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
