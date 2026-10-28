// Local browser acceptance for the real new-api / QuantumNous frontend.
// All API and media requests are fulfilled locally. No provider or production API is used.
import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { mkdir, writeFile } from 'node:fs/promises'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const require = createRequire(process.env.QA_NODE_PACKAGE || resolve(here, '../../../web/default/package.json'))
const { chromium } = require('playwright')
const browser = await chromium.launch({ channel: 'chrome', headless: true })
const base = 'http://127.0.0.1:4875'
const output = resolve(here, 'screenshots/implementation')
await mkdir(output, { recursive: true })
const context = await browser.newContext({ viewport: { width: 1440, height: 1280 } })
const page = await context.newPage()
const errors = []
const evidence = []
page.on('pageerror', error => errors.push(error.message))
page.on('console', message => { if (message.type() === 'error') errors.push(message.text()) })
await context.addInitScript(() => {
  localStorage.setItem('newapi:theme:v1:mode', 'dark')
  localStorage.setItem('i18nextLng', 'zh')
})
const now = Math.floor(Date.now() / 1000)
const user = { id: 777, username: 'visual-review', display_name: '设计验收', role: 1, group: 'default', quota: 5000000, language: 'zh' }
const ratioLabels = {
  '16:9': '横屏 16:9', '9:16': '竖屏 9:16', '1:1': '方形 1:1',
  '4:3': '横屏 4:3', '3:4': '竖屏 3:4', '21:9': '超宽屏 21:9', adaptive: '自动',
}
const taskPrompts = [
  ('生成一段严格 5 秒、16:9 的电影质感视频。雨后城市深蓝色天空，镜头沿街道缓缓推进，水珠反射暖色灯光。').repeat(8) + 'Maintain one continuous shot with consistent lighting and camera motion. '.repeat(5) + 'NoCutNoFadeNoTextNoLogoNoWatermark'.repeat(4),
  'A continuous cinematic tracking shot through a rainy city at dawn, with precise reflections and realistic motion. '.repeat(10) + 'UnbrokenEnglishTokenWithoutSpaces'.repeat(5),
  '慢镜头穿过雨后的街道，主体始终清晰，路面倒影与真实光照保持一致。'.repeat(18),
]
const model = {
  id: 1, model: 'sora-2', display_name: 'Sora 2', provider_label: 'OpenAI', description: '',
  specification_version: 1, enabled: true, sort_order: 1, created_at: now, updated_at: now,
  specification: {
    version: 1, modes: ['text_to_video', 'image_to_video', 'first_last_frame'],
    parameters: [
      { key: 'aspect_ratio', label: '画面比例', control: 'select', default: '16:9', options: Object.entries(ratioLabels).map(([value, label]) => ({ value, label })) },
      { key: 'resolution', label: '分辨率', control: 'select', default: '720p', options: ['720p', '1080p'].map(value => ({ value, label: value })) },
      { key: 'duration', label: '时长', control: 'select', default: 8, options: [4, 8, 12].map(value => ({ value, label: `${value} 秒` })) },
      { key: 'watermark', label: '添加水印', control: 'switch', default: false },
    ],
    reference_inputs: ['reference', 'first_frame', 'last_frame'].map(role => ({ role, request_key: role, required: true })),
  },
  default_parameters: { aspect_ratio: '16:9', resolution: '720p', duration: 8, watermark: false },
}
const generations = [3, 2, 1].map(id => ({
  id, task_id: `task_visual_${id}`, model_profile_id: 1, model: model.model, mode: 'text_to_video',
  prompt: taskPrompts[3 - id],
  parameters: { aspect_ratio: '16:9', resolution: '720p', duration: 8 }, status: 'ready', progress: '100%', quota: 100,
  output_asset_id: id, video_url: `/api/video-studio/assets/${id}/content`, poster_url: `/api/video-studio/assets/${id}/poster`, download_url: `/api/video-studio/assets/${id}/download`, created_at: id === 1 ? now - 45 * 86400 : now - (3 - id) * 300, updated_at: now,
}))
const samples = generations.map(g => ({ ...g, title: '城市光影', model_display_name: 'Sora 2', model_version: 1, reference_asset_ids: [], reference_content_urls: [], video_asset_id: g.id, preview_url: '', aspect_ratio: 16 / 9, category: 'other', status: 'published', sort_order: 1 }))
let scene = 'ready'
let postCount = 0
let holdInitialMedia = true
let releaseInitialMedia
const initialMediaGate = new Promise(resolve => { releaseInitialMedia = resolve })
await context.route('**/*', async route => {
  const url = new URL(route.request().url())
  if (url.origin !== base) return route.abort()
  const path = url.pathname
  if (path === '/qa/poster.webp') return route.fulfill({ path: resolve(here, 'assets/city-ride-cinematic.webp'), contentType: 'image/webp' })
  if (path === '/qa/video.mp4') return route.fulfill({ path: process.env.QA_VIDEO || '/tmp/video-studio-qa.mp4', contentType: 'video/mp4' })
  if (!/^\/(api|pg|invitations\/api)\//.test(path)) return route.continue()
  let data = {}
  if (path === '/api/site-config') data = { tenant_id: 0, hostname: '127.0.0.1', status: 'active', site_name: 'KKAI' }
  else if (path === '/api/status') data = { system_name: 'KKAI', version: 'local-visual', VideoStudioAccessMode: 'all', ImageStudioAccessMode: 'all' }
  else if (path === '/api/setup') data = { status: true }
  else if (path === '/api/notice') data = ''
  else if (path === '/api/requests/notifications') data = { items: [], unread_count: 0, total: 0 }
  else if (path.startsWith('/invitations/api/')) data = { enabled: false }
  else if (path === '/api/user/auth/refresh') data = { access_token: 'local-fixture', token_type: 'Bearer', access_expires_at: now + 36000, user, session: { sid: 'visual-only', current: true, login_method: 'password', ip: '127.0.0.1', user_agent: 'visual', created_at: now, last_active_at: now, expires_at: now + 36000 } }
  else if (path === '/api/user/self') data = user
  else if (path === '/api/video-studio/token') data = { required_group: 'video', has_usable_token: true, can_create: true, effective_models: ['sora-2'], token: { id: 17, name: 'Video', group: 'video' }, status: 'ready', created: false }
  else if (path === '/api/video-studio/models') data = [model]
  else if (path === '/api/video-studio/samples') data = { items: samples }
  else if (path === '/api/video-studio/generations') {
    if (scene === 'error') return route.fulfill({ status: 503, json: { success: false } })
    data = { items: scene === 'empty' ? [] : generations }
  } else if (path.startsWith('/api/video-studio/generations/')) data = generations.find(g => g.id === Number(path.split('/').at(-1)))
  else if (path === '/api/studio/media-urls') {
    if (holdInitialMedia) await initialMediaGate
    data = { urls: Object.fromEntries(route.request().postDataJSON().paths.map(path => [path, path.endsWith('/poster') ? '/qa/poster.webp' : '/qa/video.mp4'])), expires_at: now + 36000 }
  }
  else if (path === '/pg/videos/quote') data = { quota: 100, display_amount: '$0.24', request_hash: 'a'.repeat(64), expires_at: Math.floor(Date.now() / 1000) + 300 }
  else if (path === '/pg/videos') { postCount++; return route.fulfill({ status: 503, json: { success: false, code: 'video_submission_unknown', task_id: 'task_visual_unknown' } }) }
  return route.fulfill({ json: { success: true, data } })
})
async function capture(name) {
  await page.locator('button[aria-label*="devtools" i]').evaluateAll(buttons => {
    buttons.forEach(button => button.style.setProperty('display', 'none', 'important'))
  })
  await page.screenshot({ path: resolve(output, `${name}.png`), animations: 'disabled' })
}
async function create(search = '') {
  await page.goto(`${base}/video-studio/create${search}`)
  await page.locator('.video-studio-composer').waitFor()
}
async function assertRatioLayout(scene) {
  const layout = await page.locator('.video-studio-ratio-options').evaluate(group => {
    const groupRect = group.getBoundingClientRect()
    const buttons = [...group.querySelectorAll('button')].map(button => {
      const rect = button.getBoundingClientRect()
      const label = button.querySelector('span:last-child')?.getBoundingClientRect()
      return {
        left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom,
        contentFits: Boolean(label && label.left >= rect.left - 1 && label.right <= rect.right + 1 && label.top >= rect.top - 1 && label.bottom <= rect.bottom + 1),
      }
    })
    return {
      count: buttons.length,
      insideGroup: buttons.every(button => button.left >= groupRect.left - 1 && button.right <= groupRect.right + 1),
      contentFits: buttons.every(button => button.contentFits),
      buttonsDoNotOverlap: buttons.every((button, index) => buttons.slice(index + 1).every(other => button.right <= other.left + 1 || other.right <= button.left + 1 || button.bottom <= other.top + 1 || other.bottom <= button.top + 1)),
    }
  })
  assert.equal(layout.count, 7, `${scene}: ratio option count`)
  assert.equal(layout.insideGroup, true, `${scene}: ratio buttons inside group`)
  assert.equal(layout.contentFits, true, `${scene}: ratio labels inside buttons`)
  assert.equal(layout.buttonsDoNotOverlap, true, `${scene}: ratio buttons do not overlap`)
  const modesFit = await page.locator('.video-studio-modes button').evaluateAll(buttons => buttons.every(button => {
    const buttonRect = button.getBoundingClientRect()
    const text = button.querySelector('span')
    if (!text) return false
    const textRect = text.getBoundingClientRect()
    return text.scrollWidth <= text.clientWidth + 1 && textRect.left >= buttonRect.left - 1 && textRect.right <= buttonRect.right + 1
  }))
  assert.equal(modesFit, true, `${scene}: mode labels fit`)
  evidence.push({ scene, ...layout })
}
async function assertPageAlignment(scene, width) {
  await page.waitForFunction(width => document.documentElement.scrollWidth === width, width, { timeout: 3000 })
  const layout = await page.evaluate(() => {
    const root = document.querySelector('main#content')
    const header = root.querySelector('.video-studio-header').getBoundingClientRect()
    const body = root.querySelector('.video-studio-page-content, .video-studio-workspace').getBoundingClientRect()
    return {
      headerLeft: header.left,
      bodyLeft: body.left,
      pageFits: root.scrollWidth <= root.clientWidth + 1,
      documentWidth: document.documentElement.scrollWidth,
    }
  })
  assert.ok(Math.abs(layout.headerLeft - layout.bodyLeft) <= 2, `${scene}: header and content align`)
  assert.equal(layout.pageFits, true, `${scene}: page has no horizontal overflow`)
  assert.equal(layout.documentWidth, width, `${scene}: document has no horizontal overflow`)
  evidence.push({ scene, ...layout })
}
async function assertLibraryLayout(scene, width, expectedCounts = [2, 1]) {
  await assertPageAlignment(scene, width)
  const groups = await page.locator('section .video-studio-library-grid').evaluateAll(grids => grids.map(grid => {
    const group = grid.getBoundingClientRect()
    const date = grid.previousElementSibling.getBoundingClientRect()
    const toolbar = document.querySelector('.video-studio-page-toolbar').getBoundingClientRect()
    const cards = [...grid.querySelectorAll('.video-studio-library-card')].map(card => card.getBoundingClientRect())
    return {
      cards: cards.length,
      titleOffset: Math.abs(toolbar.left - date.left),
      dateOffset: Math.abs(date.left - cards[0].left),
      cardsFit: cards.every(card => card.left >= group.left - 1 && card.right <= group.right + 1),
    }
  }))
  assert.deepEqual(groups.map(group => group.cards), expectedCounts, `${scene}: date groups`)
  assert.ok(groups.every(group => group.titleOffset <= 2), `${scene}: dates align with page title`)
  assert.ok(groups.every(group => group.dateOffset <= 2), `${scene}: dates align with first cards`)
  assert.ok(groups.every(group => group.cardsFit), `${scene}: cards fit their groups`)
  evidence.push({ scene, groups })
}
async function assertTaskLayout(scene, width) {
  await assertPageAlignment(scene, width)
  const row = page.locator('.video-studio-task-row:not(.video-studio-task-row-head)').first()
  const collapsedHeight = await row.evaluate(element => element.getBoundingClientRect().height)
  assert.ok(collapsedHeight < (width >= 600 ? 250 : 410), `${scene}: collapsed task row remains compact`)
  const prompt = row.locator('details.video-studio-task-prompt')
  const expand = prompt.locator('summary')
  assert.equal(await expand.count(), 1, `${scene}: full prompt has an operable entry`)
  assert.equal(await expand.getAttribute('aria-label'), '提示词', `${scene}: prompt has a concise accessible name`)
  await expand.focus()
  await page.keyboard.press('Enter')
  assert.equal(await prompt.evaluate(element => element.open), true, `${scene}: keyboard opens the prompt`)
  const expandedHeight = await row.evaluate(element => element.getBoundingClientRect().height)
  assert.ok(expandedHeight > collapsedHeight + 40, `${scene}: expanding reveals the full prompt`)
  assert.equal(await prompt.locator('p').textContent(), taskPrompts[0], `${scene}: full prompt is preserved`)
  await assertPageAlignment(`${scene}-expanded`, width)
  evidence.push({ scene, collapsedHeight, expandedHeight, fullPromptLength: taskPrompts[0].length })
  await expand.click()
  assert.equal(await prompt.evaluate(element => element.open), false, `${scene}: click closes the prompt`)
}
try {
  await create()
  await page.locator('.video-studio-player-state[role="status"]').waitFor()
  assert.equal(await page.locator('.video-studio-player-state[role="alert"]').count(), 0)
  assert.equal(await page.getByRole('button', { name: '播放视频', exact: true }).count(), 0)
  evidence.push({ scene: 'initial-signature', waitingWithoutError: true, playDisabledUntilSigned: true })
  holdInitialMedia = false
  releaseInitialMedia()
  await page.getByRole('button', { name: '播放视频', exact: true }).waitFor()
  await page.locator('textarea').fill(generations[0].prompt)
  await page.locator('.video-studio-form-footer strong').filter({ hasText: '$0.24' }).waitFor()
  await assertRatioLayout('ratio-desktop')
  await assertPageAlignment('create-desktop', 1440)
  await page.locator('.video-studio-inspiration-toggle').click()
  await page.locator('main#content').evaluate(e => e.scrollTo(0, 0))
  await capture('dark-create-desktop')
  evidence.push(await page.locator('.video-studio-page').evaluate(e => ({ scene: 'desktop', page: getComputedStyle(e).backgroundColor, accent: getComputedStyle(e).getPropertyValue('--vs-accent').trim(), editorWidth: document.querySelector('.video-studio-editor').getBoundingClientRect().width, gap: getComputedStyle(document.querySelector('.video-studio-workspace')).gap })))
  await page.setViewportSize({ width: 1989, height: 1248 })
  await assertRatioLayout('ratio-wide-desktop')
  await assertPageAlignment('create-wide-desktop', 1989)
  await capture('dark-create-wide-desktop')
  await page.setViewportSize({ width: 1440, height: 1280 })
  await page.getByRole('button', { name: '播放视频', exact: true }).click()
  await page.waitForFunction(() => document.querySelector('.video-studio-player video')?.readyState >= 2)
  evidence.push(await page.locator('.video-studio-player video').evaluate(v => ({ scene: 'playback', readyState: v.readyState, duration: v.duration, controls: v.controls, fit: getComputedStyle(v).objectFit })))
  await page.getByRole('button', { name: '详情', exact: true }).click()
  await page.getByRole('dialog').waitFor()
  assert.equal(await page.locator('.video-studio-container video').count(), 0)
  await capture('dark-details-desktop')
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: '首尾帧', exact: true }).click()
  await capture('dark-first-last-frames')
  await page.getByRole('button', { name: '文生视频', exact: true }).click()
  for (const width of [1024, 768, 390, 320]) {
    await page.setViewportSize({ width, height: 844 })
    await page.locator('main#content').evaluate(e => e.scrollTo(0, 0))
    await page.waitForFunction(width => document.documentElement.scrollWidth === width, width, { timeout: 3000 })
    if (await page.evaluate(() => document.documentElement.scrollWidth) !== width) {
      console.log(await page.evaluate(() => [...document.querySelectorAll('body *')].map(e => ({ tag: e.tagName, cls: String(e.className), right: e.getBoundingClientRect().right, width: e.getBoundingClientRect().width })).filter(e => e.right > innerWidth + 1 && e.width > 0)))
      await capture(`overflow-${width}`)
    }
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth), width)
    assert.equal(await page.locator('.video-studio-composer').count(), 1)
    await assertPageAlignment(`create-${width}`, width)
    if (width === 390 || width === 320) await assertRatioLayout(`ratio-${width}`)
    await capture(`dark-create-${width}`)
    await page.locator('.video-studio-result').scrollIntoViewIfNeeded()
    await capture(`dark-result-${width}`)
    evidence.push({ scene: `responsive-${width}`, noHorizontalOverflow: true, oneComposer: true })
  }
  await page.emulateMedia({ reducedMotion: 'reduce' })
  assert.equal(await page.locator('.video-studio-recent-card').first().evaluate(e => getComputedStyle(e).transitionDuration), '0s')
  evidence.push({ scene: 'reduced-motion', transition: '0s' })
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  await page.setViewportSize({ width: 1440, height: 1280 })
  await page.goto(`${base}/video-studio/library`)
  await page.locator('.video-studio-library-grid').waitFor()
  await page.waitForFunction(() => {
    const posters = [...document.querySelectorAll('.video-studio-library-preview img')]
    return posters.length === 3 && posters.every(img => img.complete && img.naturalWidth > 0)
  })
  await page.locator('.video-studio-library-preview img').evaluateAll(async posters => {
    await Promise.all(posters.map(img => img.decode()))
    await new Promise(requestAnimationFrame)
  })
  await assertLibraryLayout('library-desktop', 1440)
  evidence.push({ scene: 'library-posters', decoded: 3 })
  await capture('dark-library-desktop')
  await page.setViewportSize({ width: 1989, height: 1248 })
  await assertLibraryLayout('library-wide-desktop', 1989)
  await capture('dark-library-wide-desktop')
  for (const width of [1024, 768]) {
    await page.setViewportSize({ width, height: 1024 })
    await assertLibraryLayout(`library-${width}`, width)
  }
  for (const width of [390, 320]) {
    await page.setViewportSize({ width, height: 844 })
    await assertLibraryLayout(`library-${width}`, width)
    await capture(`dark-library-${width}`)
  }
  generations.push({ ...generations[0], id: 4, task_id: 'task_visual_4', output_asset_id: 4, poster_url: '/api/video-studio/assets/4/poster' })
  await page.setViewportSize({ width: 1440, height: 1280 })
  await page.reload()
  await page.locator('.video-studio-library-grid').waitFor()
  await assertLibraryLayout('library-three-cards', 1440, [3, 1])
  generations.pop()
  generations[1].status = 'archiving'
  generations[2].status = 'processing'
  generations[2].progress = '42%'
  await page.setViewportSize({ width: 1440, height: 1280 })
  await page.goto(`${base}/video-studio/tasks`)
  await page.locator('.video-studio-tasks-list').waitFor()
  await assertTaskLayout('tasks-desktop', 1440)
  await page.setViewportSize({ width: 1989, height: 1248 })
  await assertTaskLayout('tasks-wide-desktop', 1989)
  await page.evaluate(() => document.activeElement?.blur())
  await capture('dark-tasks-wide-desktop')
  await page.setViewportSize({ width: 1440, height: 1280 })
  await capture('dark-tasks-desktop')
  for (const width of [390, 320]) {
    await page.setViewportSize({ width, height: 844 })
    await assertTaskLayout(`tasks-${width}`, width)
    await page.evaluate(() => document.activeElement?.blur())
    await capture(`dark-tasks-${width}`)
  }
  await page.setViewportSize({ width: 1440, height: 1280 })
  await create('?generation=1&task=task_visual_1')
  await page.locator('progress[value="42"]').waitFor()
  assert.equal(await page.locator('.video-studio-player video').count(), 0)
  await capture('dark-processing-desktop')
  await create('?generation=2&task=task_visual_2')
  await page.locator('.video-studio-player[data-status="archiving"]').waitFor()
  await capture('dark-archiving-desktop')
  scene = 'empty'
  await create()
  await page.locator('.video-studio-first-state').waitFor()
  await capture('dark-empty-desktop')
  // Use the application's real theme controls; inspectable labels are radio controls.
  await page.getByRole('button', { name: '打开主题设置', exact: true }).click()
  await page.getByRole('radio', { name: 'Select 浅色', exact: true }).click()
  await page.keyboard.press('Escape')
  await page.locator('html:not(.dark)').waitFor()
  await capture('light-empty-desktop')
  evidence.push(await page.locator('.video-studio-page').evaluate(e => ({ scene: 'light', page: getComputedStyle(e).backgroundColor, accent: getComputedStyle(e).getPropertyValue('--vs-accent').trim() })))
  assert.deepEqual(errors, [])
  assert.equal(postCount, 0)
  await writeFile(resolve(output, 'browser-evidence.json'), JSON.stringify({ date: new Date().toISOString(), browser: await browser.version(), source: 'Real React routes with local intercepted fixtures', assertions: evidence, pageErrors: errors, generationPosts: postCount }, null, 2) + '\n')
  console.log(JSON.stringify({ assertions: evidence.length, screenshots: output, errors: errors.length }, null, 2))
} finally {
  await browser.close()
}
