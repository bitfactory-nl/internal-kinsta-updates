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
