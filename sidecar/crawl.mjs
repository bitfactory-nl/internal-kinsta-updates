// crawl.mjs — bezoekt pagina's van een site en legt vast welke bestanden uit
// wp-content/uploads daadwerkelijk worden opgevraagd.
//
// Dit is het tegengif voor de blinde vlek van de databasescan: wat een pagina
// rendert, is de werkelijkheid. Sliders, pagebuilder-CSS, lazy-loaded afbeeldingen
// en door JavaScript ingevoegde beelden komen hier wél in beeld, ook als er in de
// database geen verwijzing te vinden was.
//
// Invoer (JSON op stdin):
//   { baseURL, maxPages, timeoutMs, basicAuth?: {user, pass} }
// Uitvoer (JSON op stdout):
//   { pagesVisited, uploads: { "2024/05/foto.jpg": ["https://site/pagina"] }, errors }

import { chromium } from 'playwright'

function lees(stream) {
  return new Promise((resolve, reject) => {
    let data = ''
    stream.on('data', (c) => { data += c })
    stream.on('end', () => resolve(data))
    stream.on('error', reject)
  })
}

/** Alles na "/uploads/" is het pad zoals de scan het kent, zonder querystring. */
function uploadsPad(url) {
  const i = url.indexOf('/uploads/')
  if (i < 0) return ''
  let rel = url.slice(i + '/uploads/'.length)
  const vraag = rel.search(/[?#]/)
  if (vraag >= 0) rel = rel.slice(0, vraag)
  try {
    rel = decodeURIComponent(rel)
  } catch {
    // een pad dat niet te decoderen is, nemen we zoals het is
  }
  return rel
}

/** Paginalijst uit de WordPress-sitemap; die dekt veel beter dan links volgen. */
async function uitSitemap(page, baseURL, maxPages) {
  const kandidaten = ['/wp-sitemap.xml', '/sitemap_index.xml', '/sitemap.xml']
  const paginas = []
  for (const pad of kandidaten) {
    let xml = ''
    try {
      const resp = await page.request.get(new URL(pad, baseURL).toString(), { timeout: 15000 })
      if (!resp.ok()) continue
      xml = await resp.text()
    } catch {
      continue
    }
    const locs = [...xml.matchAll(/<loc>([^<]+)<\/loc>/g)].map((m) => m[1].trim())
    if (!locs.length) continue

    // Een sitemap-index verwijst naar andere sitemaps; die één niveau diep volgen.
    const subs = locs.filter((l) => /\.xml($|\?)/i.test(l))
    const directe = locs.filter((l) => !/\.xml($|\?)/i.test(l))
    paginas.push(...directe)
    for (const sub of subs.slice(0, 20)) {
      if (paginas.length >= maxPages) break
      try {
        const resp = await page.request.get(sub, { timeout: 15000 })
        if (!resp.ok()) continue
        const subXml = await resp.text()
        paginas.push(...[...subXml.matchAll(/<loc>([^<]+)<\/loc>/g)]
          .map((m) => m[1].trim())
          .filter((l) => !/\.xml($|\?)/i.test(l)))
      } catch {
        // een onbereikbare sub-sitemap slaan we over
      }
    }
    if (paginas.length) break
  }
  return [...new Set(paginas)].slice(0, maxPages)
}

/** Scrollen tot onderaan, zodat lazy-loaded afbeeldingen ook echt geladen worden. */
async function scrollDoor(page) {
  await page.evaluate(async () => {
    const stap = Math.max(400, Math.floor(window.innerHeight * 0.8))
    for (let y = 0; y < document.body.scrollHeight; y += stap) {
      window.scrollTo(0, y)
      await new Promise((r) => setTimeout(r, 120))
    }
    window.scrollTo(0, 0)
  })
  await page.waitForTimeout(400)
}

async function main() {
  const req = JSON.parse(await lees(process.stdin))
  const baseURL = req.baseURL
  const maxPages = Math.max(1, Math.min(req.maxPages || 40, 500))
  const timeoutMs = req.timeoutMs || 20000

  const browser = await chromium.launch()
  const context = await browser.newContext({
    httpCredentials: req.basicAuth ? { username: req.basicAuth.user, password: req.basicAuth.pass } : undefined,
    ignoreHTTPSErrors: true,
  })
  const page = await context.newPage()

  const uploads = {}
  const errors = []
  let huidigePagina = ''

  page.on('response', (r) => {
    const rel = uploadsPad(r.url())
    if (!rel) return
    if (!uploads[rel]) uploads[rel] = []
    // Per bestand hoogstens vijf vindplaatsen: genoeg om te zien waar het staat,
    // zonder dat het resultaat opzwelt bij een bestand in de footer van elke pagina.
    if (uploads[rel].length < 5 && !uploads[rel].includes(huidigePagina)) {
      uploads[rel].push(huidigePagina)
    }
  })

  let paginas = []
  try {
    paginas = await uitSitemap(page, baseURL, maxPages)
  } catch (e) {
    errors.push('sitemap: ' + String(e.message || e))
  }

  // Zonder sitemap: de homepage plus de interne links die daarop staan.
  if (!paginas.length) {
    paginas = [baseURL]
    try {
      huidigePagina = baseURL
      await page.goto(baseURL, { waitUntil: 'domcontentloaded', timeout: timeoutMs })
      const links = await page.evaluate(() =>
        [...document.querySelectorAll('a[href]')].map((a) => a.href))
      // De vergelijking moet tegen baseURL: `location` bestaat hier niet, dit draait
      // in Node en niet in de pagina.
      const eigenOrigin = new URL(baseURL).origin
      const eigen = links.filter((l) => {
        try {
          return new URL(l).origin === eigenOrigin && !/\.(jpe?g|png|gif|pdf|zip|mp4)$/i.test(l)
        } catch {
          return false
        }
      })
      paginas.push(...[...new Set(eigen)])
    } catch (e) {
      errors.push(baseURL + ': ' + String(e.message || e))
    }
    paginas = [...new Set(paginas)].slice(0, maxPages)
  }

  let bezocht = 0
  for (const url of paginas) {
    huidigePagina = url
    try {
      await page.goto(url, { waitUntil: 'domcontentloaded', timeout: timeoutMs })
      await scrollDoor(page)
      bezocht++
    } catch (e) {
      errors.push(url + ': ' + String(e.message || e))
    }
  }

  await browser.close()
  process.stdout.write(JSON.stringify({
    pagesVisited: bezocht,
    pagesPlanned: paginas.length,
    uploads,
    errors: errors.slice(0, 50),
  }))
}

main().catch((e) => {
  process.stdout.write(JSON.stringify({ pagesVisited: 0, uploads: {}, errors: [String(e.message || e)] }))
  process.exit(0)
})
