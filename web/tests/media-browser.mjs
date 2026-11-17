// Real Server + test Adapter only. Never launch a production model provider.
// Usage: node web/tests/media-browser.mjs <compiled-test-fixture> <rps> <chromium>
import assert from 'node:assert/strict'
import fs from 'node:fs/promises'
import path from 'node:path'
import os from 'node:os'
import { fileURLToPath } from 'node:url'
import { chromium } from 'playwright'
import { launchFixture, launchVite, eventually } from '../../tests/phase3-support.mjs'
const repo = fileURLToPath(new URL('../../', import.meta.url))
const [worker, cli, executablePath] = process.argv.slice(2)
if (!worker || !cli) throw new Error('Explicit test fixture and CLI paths are required; no real Provider may be used.')
const evidence = path.join(repo, 'web/test-results/phase6'), temp = await fs.mkdtemp(path.join(os.tmpdir(), 'rps-web-media-'))
await fs.mkdir(evidence, { recursive: true })
let fixture, vite, browser, page
try {
  fixture = await launchFixture({ worker, cli, temp, evidence, mode: 'web-media' })
  if (process.env.RPS_TEST_MODE !== 'embed') vite = await launchVite({ repo, target: fixture.url, evidence })
  browser = await chromium.launch({ headless: true, executablePath })
  page = await browser.newPage({ viewport: { width: 1440, height: 1000 } })
  const errors = []; page.on('pageerror', e => errors.push(e.message))
  const nav = async name => { if (await page.getByRole('dialog').count()) await page.getByRole('button', { name: '关闭面板', exact: true }).click(); await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: new RegExp(name+'$') }).click() }
  await page.goto(`${vite?.url || fixture.url}/#token=${encodeURIComponent(fixture.token)}`)
  await page.getByRole('navigation', { name: '主导航' }).waitFor()
  assert.equal(new URL(page.url()).hash, '')
  const files = path.join(repo, 'tests/phase6-fixtures')
  await page.getByLabel('上传剧情附件', { exact: true }).setInputFiles(path.join(files, 'pixel.png'))
  await page.getByRole('region', { name: '剧情与交互' }).waitFor()
  await page.getByLabel('待发送附件').locator('img').waitFor()
  await eventually(() => page.getByLabel('待发送附件').locator('img').evaluate(i => i.complete && i.naturalWidth > 0))
  for (const [filename, tag] of [['tone.wav', 'audio'], ['clip.webm', 'video']]) {
    await page.getByLabel('上传剧情附件', { exact: true }).setInputFiles(path.join(files, filename))
    await page.getByLabel('待发送附件').locator(tag).waitFor()
    await eventually(() => page.getByLabel('待发送附件').locator(tag).evaluate(m => m.readyState >= 1), { label: filename + ' actual decoded metadata' })
  }
  await page.getByLabel('上传剧情附件', { exact: true }).setInputFiles({ name: 'unsafe.svg', mimeType: 'image/svg+xml', buffer: Buffer.from('<svg xmlns="http://www.w3.org/2000/svg" onload="window.pwned=true"></svg>') })
  await page.getByRole('button', { name: '下载附件 unsafe.svg', exact: true }).waitFor()
  assert.equal(await page.getByLabel('待发送附件').locator('iframe,object,embed,svg').count(), 0)
  assert.equal(await page.evaluate(() => window.pwned), undefined)
  const mediaDownload = page.waitForEvent('download')
  await page.getByRole('button', { name: '下载附件 unsafe.svg', exact: true }).click()
  const downloadedSVG = await mediaDownload; assert.equal(await downloadedSVG.failure(), null)
  await page.getByRole('button', { name: '发送提示', exact: true }).click()
  const input = await eventually(async () => ((await fixture.api('/chats/main/events')).json.data ?? []).find(e => e.type === 'input' && e.media?.length))
  await fixture.task(input.id)
  await fixture.action(input.id, 'emit', { text: '测试 Adapter 正式正文；不代表模型输出。', complete: true })
  await page.locator(`[data-event-id="${input.id}"] img`).waitFor()
  await page.getByLabel(`消息操作 ${input.id}`, {exact:true}).click();await page.getByRole('button', { name: `重新生成事件 ${input.id}`, exact: true }).click()
  await page.getByLabel('重新生成故事 ID', { exact: true }).fill('p6_regen')
  await page.getByLabel('重新生成故事名称', { exact: true }).fill('重新生成测试')
  await page.getByLabel('在新线重新提交原提示', { exact: true }).uncheck()
  await page.getByRole('button', { name: '确认重新生成分支', exact: true }).click()
  await eventually(() => page.getByLabel('当前故事', { exact: true }).inputValue().then(v => v === 'p6_regen'))
  assert.equal(((await fixture.api('/chats/p6_regen/events?inherited=true')).json.data ?? []).length, 0)
  await page.getByLabel('当前故事', { exact: true }).selectOption('main')
  await nav('角色')
  await page.getByLabel('角色 ID', { exact: true }).fill('media_character')
  await page.getByLabel('角色名称', { exact: true }).fill('媒体角色')
  await page.getByRole('button', { name: '保存角色卡', exact: true }).click()
  await page.getByLabel('角色媒体文件', { exact: true }).waitFor()
  await page.getByLabel('角色媒体文件', { exact: true }).setInputFiles(path.join(files, 'pixel.png'))
  await page.getByLabel('媒体用途', { exact: true }).selectOption('avatar')
  await page.getByRole('button', { name: '上传角色媒体', exact: true }).click()
  await page.getByText('当前头像', { exact: true }).waitFor()
  await page.getByLabel('角色媒体文件', { exact: true }).setInputFiles(path.join(files, 'pixel.png'))
  await page.getByLabel('媒体用途', { exact: true }).selectOption('expression')
  await page.getByLabel('表情 ID', { exact: true }).fill('happy')
  await page.getByRole('button', { name: '上传角色媒体', exact: true }).click()
  await page.getByText('表情 happy', { exact: true }).waitFor()
  const downloadPromise = page.waitForEvent('download')
  await page.getByRole('button', { name: '导出角色', exact: true }).click()
  const archive = await downloadPromise, archivePath = path.join(temp, 'character.zip')
  await archive.saveAs(archivePath)
  assert.ok((await fs.stat(archivePath)).size > 0)
  await page.getByRole('button', { name: '删除角色媒体 pixel.png', exact: true }).count().then(n => assert.equal(n, 2))
  await nav('故事')
  const lineDownload = page.waitForEvent('download')
  await page.getByRole('button', { name: '导出故事', exact: true }).click()
  const exported = await lineDownload, timelinePath = path.join(temp, 'timeline.zip')
  await exported.saveAs(timelinePath)
  await page.getByLabel('故事导入文件', { exact: true }).setInputFiles(timelinePath)
  await page.getByRole('button', { name: '导入故事档案', exact: true }).click()
  await page.getByRole('alert').waitFor()
  assert.ok((await page.getByRole('alert').textContent()).includes('conflict'))
  await page.getByRole('button', { name: '关闭错误', exact: true }).click()
  for (const size of [{ width: 390, height: 844 }, { width: 320, height: 720 }]) {
    await page.setViewportSize(size); await nav('剧情')
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true)
    await page.screenshot({ path: path.join(evidence, `media-${size.width}.png`), fullPage: true })
  }
  assert.deepEqual(errors, []); assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0)
  console.log(`PASS real ${vite ? 'Vite proxy' : 'Go embed'}: fragment auth, PNG/WAV/WebM actual metadata + SVG download-only, media-only input + actual CLI output, input-before branch, avatar/expression, character/line ZIP download + conflict, mobile320/390. No real models.`)
} catch (e) {
  if (page) { await page.screenshot({ path: path.join(evidence, 'failure.png'), fullPage: true }); console.error((await page.locator('body').innerText()).slice(-7000)) }
  throw e
} finally {
  await browser?.close(); await vite?.stop(); await fixture?.stop(); await fs.rm(temp, { recursive: true, force: true })
}
