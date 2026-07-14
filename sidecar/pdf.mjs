// Reads {html, path} (JSON) from stdin, renders the HTML to a PDF with
// headless Chromium, and writes {ok, path} (or {error}) as JSON to stdout.
// Mirrors runner.mjs's stdin/stdout JSON contract.
import { chromium } from 'playwright'
import { readFileSync } from 'node:fs'

function readStdin() {
  return JSON.parse(readFileSync(0, 'utf8'))
}

async function main() {
  const req = readStdin()
  const browser = await chromium.launch()
  try {
    const page = await browser.newPage()
    await page.setContent(req.html, { waitUntil: 'networkidle' })
    await page.pdf({
      path: req.path,
      format: 'A4',
      printBackground: true,
      margin: { top: '18mm', bottom: '18mm', left: '16mm', right: '16mm' },
    })
    process.stdout.write(JSON.stringify({ ok: true, path: req.path }))
  } finally {
    await browser.close().catch(() => {})
  }
}

main().catch((e) => {
  process.stdout.write(JSON.stringify({ error: String(e && e.message ? e.message : e) }))
  process.exit(0)
})
