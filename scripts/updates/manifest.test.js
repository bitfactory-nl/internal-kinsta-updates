'use strict'

const { test } = require('node:test')
const assert = require('node:assert/strict')
const {
  renderNpmMajorsSection,
  renderManualSection,
  renderWpAppliedSection,
  collectManualWpItems,
} = require('./manifest')

test('renderNpmMajorsSection lists majors, or empty when none', () => {
  assert.equal(renderNpmMajorsSection([]), '')
  const s = renderNpmMajorsSection([{ name: 'eslint', from: '9.39.2', to: '10.7.0' }])
  assert.match(s, /Beschikbare major updates/)
  assert.match(s, /NIET automatisch uitgevoerd/)
  assert.match(s, /eslint.*9\.39\.2.*10\.7\.0/)
})

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

test('workflow embeds de canonieke scripts en renderers (drift guard)', () => {
  const fs = require('node:fs')
  const path = require('node:path')
  const norm = (s) => s.split('\n').map((l) => l.trim()).filter(Boolean).join('\n')
  const wf = norm(fs.readFileSync(path.join(__dirname, '../../.github/workflows/check-updates.yml'), 'utf8'))

  // Whole-file embeds (zelfstandige /tmp-scripts)
  for (const file of ['wp-apply-runner.mjs', 'build-manifest.mjs']) {
    const src = norm(fs.readFileSync(path.join(__dirname, file), 'utf8'))
    assert.ok(wf.includes(src), `embedded copy van ${file} wijkt af — synchroniseer check-updates.yml`)
  }

  // Function-level embeds uit manifest.js (github-script)
  const src = fs.readFileSync(path.join(__dirname, 'manifest.js'), 'utf8')
  for (const name of ['renderNpmMajorsSection', 'renderManualSection', 'renderWpAppliedSection', 'collectManualWpItems']) {
    const m = src.match(new RegExp('^function ' + name + '[\\s\\S]*?^}', 'm'))
    assert.ok(m, `functie ${name} niet gevonden in manifest.js`)
    assert.ok(wf.includes(norm(m[0])), `embedded copy van ${name} wijkt af — synchroniseer check-updates.yml`)
  }
})
