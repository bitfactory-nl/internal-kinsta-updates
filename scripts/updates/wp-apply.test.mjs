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
