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
