'use strict'

// LET OP: .github/workflows/check-updates.yml bevat een embedded copy van
// deze functies (een reusable workflow kan dit bestand niet require'n omdat
// de workspace de klant-repo bevat). Wijzig je hier iets, werk dan ook de
// workflow bij — de drift-test in manifest.test.js dwingt dit af.

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

module.exports = { parseWpUpdates, classifyBump, computeNpmUpdates, buildManifest, renderNpmMajorsSection }
