// Reads a RunRequest (JSON) from stdin, replays the flow on two environments,
// and writes a RunResponse (JSON) to stdout. Deterministic replay: uses the
// step's cached selector when present, else a natural-language fallback. On a
// step failure it records the error + an accessibility snapshot and stops.
import { chromium } from 'playwright'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

function readStdin() {
  return JSON.parse(readFileSync(0, 'utf8'))
}

async function openSide(target, timeout) {
  const browser = await chromium.launch()
  const context = await browser.newContext({
    ignoreHTTPSErrors: true,
    httpCredentials: target.basicAuth
      ? { username: target.basicAuth.user, password: target.basicAuth.pass }
      : undefined,
  })
  const page = await context.newPage()
  page.setDefaultTimeout(timeout)
  const consoleErrors = []
  page.on('console', (m) => { if (m.type() === 'error') consoleErrors.push(m.text()) })
  page.on('pageerror', (e) => consoleErrors.push(String(e)))
  const statusCodes = {}
  page.on('response', (r) => { statusCodes[r.url()] = r.status() })
  return { browser, context, page, consoleErrors, statusCodes, baseURL: target.url }
}

// Resolve a locator for click/type/assert: prefer the cached CSS selector,
// else fall back to a text/label match from the natural-language target.
function locate(page, step) {
  if (step.selector && step.selector.trim() !== '') return page.locator(step.selector)
  return page.getByText(step.target, { exact: false }).first()
}

async function applyStep(side, step, testAccount) {
  const { page, baseURL } = side
  switch (step.action) {
    case 'navigate':
      await page.goto(new URL(step.target, baseURL).toString(), { waitUntil: 'domcontentloaded' })
      break
    case 'click':
      await locate(page, step).click()
      break
    case 'type':
      await locate(page, step).fill(step.value)
      break
    case 'login': {
      const path = step.target && step.target.trim() !== '' ? step.target : '/wp-login.php'
      await page.goto(new URL(path, baseURL).toString(), { waitUntil: 'domcontentloaded' })
      if (testAccount) {
        await page.fill('#user_login', testAccount.user)
        await page.fill('#user_pass', testAccount.pass)
        await page.click('#wp-submit')
        await page.waitForLoadState('domcontentloaded')
      }
      break
    }
    case 'wait': {
      const ms = Number(step.target)
      if (!Number.isNaN(ms) && ms > 0) await page.waitForTimeout(ms)
      else if (step.target) await locate(page, step).waitFor()
      break
    }
    case 'assert':
      await locate(page, step).waitFor({ state: 'visible' })
      break
    default:
      throw new Error(`unknown action: ${step.action}`)
  }
}

async function shoot(side, dir, name) {
  const path = join(dir, name)
  await side.page.screenshot({ path, fullPage: true })
  return path
}

async function main() {
  const req = readStdin()
  const timeout = req.timeoutMs && req.timeoutMs > 0 ? req.timeoutMs : 30000
  const resp = { steps: [], error: '' }

  let baseline, update
  try {
    baseline = await openSide(req.baseline, timeout)
    update = await openSide(req.update, timeout)

    for (let i = 0; i < req.flow.steps.length; i++) {
      const step = req.flow.steps[i]
      const result = {
        index: i,
        action: step.action,
        baseline: { screenshot: '', consoleErrors: [], statusCodes: {} },
        update: { screenshot: '', consoleErrors: [], statusCodes: {} },
        error: '',
        snapshot: '',
      }
      try {
        await applyStep(baseline, step, req.testAccount)
        await applyStep(update, step, req.testAccount)
      } catch (e) {
        result.error = String(e && e.message ? e.message : e)
        try { result.snapshot = JSON.stringify(await update.page.accessibility.snapshot()) } catch {}
      }
      try { result.baseline.screenshot = await shoot(baseline, req.screenshotDir, `s${i}-baseline.png`) } catch {}
      try { result.update.screenshot = await shoot(update, req.screenshotDir, `s${i}-update.png`) } catch {}
      result.baseline.consoleErrors = [...baseline.consoleErrors]
      result.update.consoleErrors = [...update.consoleErrors]
      result.baseline.statusCodes = { ...baseline.statusCodes }
      result.update.statusCodes = { ...update.statusCodes }
      resp.steps.push(result)
      if (result.error) break
    }
  } catch (e) {
    resp.error = String(e && e.message ? e.message : e)
  } finally {
    if (baseline) await baseline.browser.close().catch(() => {})
    if (update) await update.browser.close().catch(() => {})
  }

  process.stdout.write(JSON.stringify(resp))
}

main().catch((e) => {
  process.stdout.write(JSON.stringify({ steps: [], error: String(e && e.message ? e.message : e) }))
  process.exit(0)
})
