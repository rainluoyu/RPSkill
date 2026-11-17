import assert from 'node:assert/strict'
import { mkdir } from 'node:fs/promises'
import { preview } from 'vite'
import { chromium, firefox } from 'playwright'

// With RPS_TEST_BASE_URL/RPS_TEST_TOKEN this verifies a real server without
// mutating workspace data. Without them it tests the built login shell only.
let server, browser
const target = process.env.RPS_TEST_BASE_URL
const token = process.env.RPS_TEST_TOKEN
const engine = process.env.RPS_TEST_BROWSER || 'chromium'
assert.ok(['chromium', 'firefox'].includes(engine), 'RPS_TEST_BROWSER must be chromium or firefox')
const browserType = engine === 'firefox' ? firefox : chromium
try {
  if (!target) server = await preview({ preview: { host: '127.0.0.1', port: 0, open: false, proxy: {} } })
  const base = target || server.resolvedUrls.local[0]
  browser = await browserType.launch({ headless: true, executablePath: process.argv[2] })
  console.log(`${engine} ${browser.version()} · ${target ? 'real server, read-only smoke' : 'static login shell only'}`)
  await mkdir('test-results', { recursive: true })
  for (const viewport of [{ width: 1440, height: 1080, name: 'desktop' }, { width: 390, height: 844, name: 'mobile' }, { width: 320, height: 720, name: 'narrow-mobile' }]) {
    const page = await browser.newPage({ viewport, locale: 'zh-CN' })
    const errors = []; page.on('pageerror', (e) => errors.push(e.message))
    await page.goto(`${base}${token ? `#token=${encodeURIComponent(token)}` : ''}`)
    if (target && token) {
      await page.getByRole('navigation', { name: '主导航' }).waitFor()
      assert.equal(new URL(page.url()).hash, '')
      for (const panel of ['世界书', '角色', '故事', '主题', '剧情']) {
        if (await page.getByRole('dialog').count()) await page.getByRole('button', { name: '关闭面板', exact: true }).click()
        await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: new RegExp(panel+'$') }).click()
      }
      await page.getByLabel('创作提示', { exact: true }).waitFor()
      assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0)
    } else {
      await page.getByLabel('访问令牌').waitFor()
      assert.equal(await page.getByRole('button', { name: '登录', exact: true }).isDisabled(), true)
    }
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, `${viewport.name}: horizontal overflow`)
    assert.deepEqual(errors, [])
    await page.screenshot({ path: `test-results/phase3-${viewport.name}.png`, fullPage: true })
    console.log(`PASS ${viewport.name} ${viewport.width}×${viewport.height}`)
    await page.close()
  }
} finally {
  await browser?.close()
  if (server) await new Promise((resolve, reject) => server.httpServer.close((e) => e ? reject(e) : resolve()))
}
