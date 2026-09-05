#!/usr/bin/env node
'use strict';
// Real Chromium against the shipped HTML/SDK and explicitly synthetic API fixtures.
// No Telegram account or production endpoint is used. Run with:
// PLAYWRIGHT_MODULE=/path/to/playwright CHROMIUM=/usr/bin/chromium \
//   node scripts/tests/miniapp_browser.cjs /path/to/evidence
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const http = require('node:http');
const vm = require('node:vm');
const {execFileSync} = require('node:child_process');
const {chromium} = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const root = path.resolve(__dirname, '../..');
const out = path.resolve(process.argv[2]);
fs.mkdirSync(out, {recursive: true});
// Optional baseline comes from git in memory, never resets or writes the worktree.
const sourcePath = 'internal/miniapp/web/index.html';
const html = process.env.SOURCE_REV
  ? execFileSync('git', ['show', `${process.env.SOURCE_REV}:${sourcePath}`], {cwd: root, encoding: 'utf8'})
  : fs.readFileSync(path.join(root, sourcePath), 'utf8');
const sdk = fs.readFileSync(path.join(root, 'internal/miniapp/web/telegram-web-app.js'));
const scripts = [...html.matchAll(/<script\b([^>]*)>([\s\S]*?)<\/script>/g)].filter(m => !/\bsrc=/.test(m[1]));
assert.equal(scripts.length, 1, 'one executable inline application script');
scripts.forEach(m => new vm.Script(m[2], {filename: sourcePath}));
new vm.Script(sdk.toString(), {filename: 'telegram-web-app.js'});
const fixtureAuth = 'fixture-init-data-not-a-real-signature';
const media = [
  {tmdb_id: 101, type: 'movie', title: '未入库测试电影', year: '2026', media_status: {code: 'available', text: '可求片'}},
  {tmdb_id: 102, type: 'movie', title: '已入库测试电影', year: '2025', media_status: {code: 'in_library', text: '已在库'}},
  {tmdb_id: 103, type: 'tv', title: '分页测试剧集', year: '2024', media_status: {code: 'available', text: '可求片'}, seasons: [
    {number: 1, episode_count: 8, status: {code: 'available', text: '可求片'}},
    {number: 2, episode_count: 6, status: {code: 'in_library', text: '已在库'}}
  ]}
];
const tasks = [
  {request_id: 'fixture-pending', tmdb_id: 101, media_title: '待审核测试电影', media_year: 2026, media_type: 'movie', business_type: 'request', status: 'pending', status_group: 'pending', status_text: '等待审核', can_cancel: true},
  {request_id: 'fixture-library', tmdb_id: 103, media_title: '等待入库测试剧集', media_year: 2024, media_type: 'tv', season: 2, business_type: 'request', status: 'awaiting_library', status_group: 'active', status_text: '等待 Emby 入库', can_cancel: false},
  {request_id: 'fixture-wash', tmdb_id: 102, media_title: '已完成洗版', media_year: 2025, media_type: 'movie', business_type: 'wash', status: 'completed', status_group: 'done', status_text: '洗版完成', can_cancel: false},
  {request_id: 'fixture-ended', tmdb_id: 101, media_title: '已撤回测试电影', media_type: 'movie', business_type: 'request', status: 'cancelled', status_group: 'done', status_text: '已撤回', can_cancel: false}
];
const requests = [];
const errors = [];
const report = {kind: 'synthetic API fixtures; real Chromium; not authenticated phone acceptance', viewport: {width: 390, height: 844}, cases: [], screenshots: [], console_errors: errors};
let sdkDelay = 0;
let issueFailure = false;
let page, browser;
const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, 'http://fixture.test');
  const send = (status, body, type = 'application/json') => {res.writeHead(status, {'Content-Type': type}); res.end(type === 'application/json' ? JSON.stringify(body) : body);};
  if (url.pathname === '/miniapp') return send(200, html, 'text/html; charset=utf-8');
  if (url.pathname === '/miniapp/telegram-web-app.js') {
    await new Promise(resolve => setTimeout(resolve, sdkDelay));
    return send(200, sdk, 'text/javascript');
  }
  if (!url.pathname.startsWith('/api/miniapp/v1/')) return send(404, {message: 'fixture path not found'});
  let body = '';
  for await (const chunk of req) body += chunk;
  const entry = {path: url.pathname, query: Object.fromEntries(url.searchParams), method: req.method, auth: req.headers['x-telegram-init-data'] === fixtureAuth, body: body ? JSON.parse(body) : null};
  requests.push(entry);
  if (!entry.auth) return send(401, {message: 'Mini App 会话已过期，请从 Telegram 重新打开'});
  switch (url.pathname.split('/').pop()) {
    case 'search': {const n = Number(url.searchParams.get('page') || 1); return send(200, {results: n === 1 ? media.slice(0, 2) : media.slice(2), has_more: n === 1, next_page: n + 1});}
    case 'detail': {const item = media.find(m => m.tmdb_id === Number(url.searchParams.get('id'))); return send(item ? 200 : 404, item ? {...item, overview: '测试简介。'.repeat(40)} : {message: '未找到测试详情'});}
    case 'me': return send(200, {requests: tasks, user: {first_name: 'Fixture'}});
    case 'progress': return send(200, {request_id: url.searchParams.get('request_id'), events: [{code: 'created', text: '已提交测试任务', at: '2026-09-01T10:00:00Z'}, {code: 'download_complete', text: '下载完成，等待入库', at: '2026-09-02T10:00:00Z'}]});
    case 'dynamic': return send(200, {recently_added: [], recent_requests: []});
    case 'discover': return send(200, {featured: []});
    case 'issues':
      if (req.method === 'POST') return send(issueFailure ? 503 : 201, issueFailure ? {message: '测试反馈暂时失败'} : {ok: true});
      return send(200, {items: []});
    default: return send(404, {message: 'unexpected fixture API; writes are not allowed here'});
  }
});
function eq(actual, expected, why = 'rendered behavior matches the fixture contract') {assert.deepEqual(actual, expected, why);}
async function snapshot(name) {
  const metrics = await page.evaluate(() => ({innerWidth, scrollWidth: document.documentElement.scrollWidth, view: S.view, mode: S.mode, detail: S.detailVisible}));
  eq(metrics.scrollWidth, metrics.innerWidth, `${name}: no horizontal overflow`);
  const file = path.join(out, name + '.png');
  await page.screenshot({path: file, fullPage: false});
  report.screenshots.push({file, ...metrics});
}
async function run(name, fn) {
  try {await fn(); report.cases.push({name, pass: true}); console.log('PASS', name);}
  catch (error) {report.cases.push({name, pass: false, error: error.message}); console.error('FAIL', name, error.message); throw error;}
}
(async () => {
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const base = `http://127.0.0.1:${server.address().port}/miniapp`;
  const authHash = '#tgWebAppData=' + encodeURIComponent(fixtureAuth) + '&tgWebAppVersion=8.0&tgWebAppPlatform=web';
  browser = await chromium.launch({executablePath: process.env.CHROMIUM || '/usr/bin/chromium', headless: true, args: ['--no-sandbox']});
  report.chromium = browser.version();
  page = await browser.newPage({viewport: report.viewport, deviceScaleFactor: 1});
  page.on('pageerror', e => errors.push({type: 'pageerror', message: e.message}));
  page.on('console', msg => {if (msg.type() === 'error') errors.push({type: 'console', message: msg.text()});});
  await run('ordinary search reaches results and keeps mode through pagination/detail/back/dock', async () => {
    await page.goto(base + authHash);
    await page.locator('.studio-brand h1').waitFor();
    await page.locator('#q').fill('fixture query');
    await page.locator('#q').press('Enter');
    await page.locator('.yh-result').nth(1).waitFor();
    eq(await page.locator('.result-action').allTextContents(), ['提交求片', '查看详情']);
    eq(await page.locator('.yh-pill').allTextContents(), ['可求片', '已在库']);
    eq(await page.locator('.yh-dock [data-view="search"]').innerText(), '搜索求片');
    await snapshot('request-results');
    await page.locator('.load-more').click();
    await page.locator('.yh-result').nth(2).waitFor();
    eq(requests.filter(x => x.path.endsWith('/search')).map(x => x.query.page), ['1', '2']);
    await page.locator('.yh-result[data-mid="101"]').click();
    await page.locator('.yh-detail .yh-title').waitFor();
    eq(await page.locator('.yh-detail-actions button').first().innerText(), '提交求片');
    await page.locator('.detail-back button').click();
    eq(await page.locator('.yh-result').count(), 3);
    await page.locator('.yh-dock [data-view="search"]').click();
    eq(await page.evaluate(() => [S.mode, S.query, S.results.length, S.nextPage]), ['request', 'fixture query', 3, 3]);
  });
  await run('wash dock, results, pagination, detail/back and return dock preserve mode', async () => {
    await page.locator('[data-mode="wash"]').click();
    eq(await page.locator('.yh-dock [data-view="search"]').innerText(), '搜索洗版');
    eq((await page.locator('.yh-pill').allTextContents()).slice(0, 2), ['尚未入库', '已在库']);
    eq((await page.locator('.result-action').allTextContents()).slice(0, 2), ['不可洗版', '申请洗版']);
    eq(await page.locator('.yh-result[data-mid="101"] .result-action').getAttribute('aria-disabled'), 'true');
    eq(await page.locator('.yh-result[data-mid="102"] .result-action').getAttribute('aria-disabled'), null);
    assert.ok(!(await page.locator('.yh-list').innerText()).includes('可求片'));
    await page.locator('#q').fill('wash fixture');
    await page.locator('#q').press('Enter');
    await page.locator('.yh-result').nth(1).waitFor();
    await page.locator('#q').blur();
    await snapshot('wash-results');
    await page.locator('.load-more').click();
    await page.locator('.yh-result').nth(2).waitFor();
    eq(await page.evaluate(() => S.mode), 'wash');
    await page.locator('.yh-result[data-mid="102"]').click();
    await page.locator('.yh-detail .yh-title').waitFor();
    eq(await page.locator('.yh-detail-actions button').first().innerText(), '申请洗版');
    await snapshot('wash-detail');
    await page.locator('.detail-back button').click();
    eq(await page.evaluate(() => [S.mode, S.results.length]), ['wash', 3]);
    await page.locator('.yh-result[data-mid="102"]').click();
    await page.locator('.yh-detail .yh-title').waitFor();
    await page.locator('.yh-dock [data-view="search"]').click();
    eq(await page.evaluate(() => [S.mode, S.detailVisible, S.results.length, S.query]), ['wash', false, 3, 'wash fixture']);
  });
  await run('new progress cards expose identity, stage, metadata, terminal states and fixture event expansion', async () => {
    await page.locator('.yh-dock [data-view="tasks"]').click();
    await page.locator('.yh-task').nth(3).waitFor();
    eq(await page.evaluate(() => S.mode), 'request');
    const task = page.locator('.yh-task').filter({has: page.locator('h2', {hasText: '等待入库测试剧集'})});
    eq(await task.locator('.yh-kicker').innerText(), '求片任务');
    assert.match(await task.locator('.task-meta').innerText(), /2024.*剧集内容.*第 2 季/);
    eq(await task.locator('.yh-step b').allTextContents(), ['提交', '审核', '处理中', '入库']);
    eq(await task.locator('.yh-node.is-done').count(), 3);
    eq(await task.locator('.yh-node.is-now').count(), 1);
    eq(await task.locator('.yh-step').nth(3).locator('small').innerText(), '等待 Emby 入库');
    const pending = page.locator('.yh-task').filter({hasText: '待审核测试电影'});
    eq(await pending.locator('.yh-node.is-done').count(), 0);
    eq(await pending.locator('.yh-node.is-now').count(), 1);
    eq(await pending.locator('.task-row-actions button').allTextContents(), ['查看详情', '撤回任务']);
    const wash = page.locator('.yh-task').filter({hasText: '已完成洗版'});
    eq(await wash.locator('.yh-kicker').innerText(), '洗版任务');
    eq(await wash.locator('.yh-step b').allTextContents(), ['创建', '审核', '核验', '完成']);
    eq(await wash.locator('.yh-node.is-done').count(), 4);
    eq(await wash.locator('.yh-node.is-now').count(), 0);
    const ended = page.locator('.yh-task').filter({hasText: '已撤回测试电影'});
    eq(await ended.locator('.task-ended').innerText(), '任务已撤回');
    eq(await ended.locator('.yh-node').count(), 0);
    await task.locator('.timeline-toggle').click();
    await task.locator('.timeline-item').nth(1).waitFor();
    eq(await task.locator('.timeline-toggle').getAttribute('aria-expanded'), 'true');
    eq(await task.locator('.timeline-item strong').allTextContents(), ['已提交测试任务', '下载完成，等待入库']);
    eq(requests.filter(x => x.path.endsWith('/progress')).at(-1).query.request_id, 'fixture-library');
    await task.scrollIntoViewIfNeeded();
    await snapshot('progress-timeline');
    await task.locator('.timeline-toggle').click();
    eq(await task.locator('.timeline-item').count(), 0);
    await task.locator('.task-row-actions button').first().click();
    await page.locator('.yh-season-list').waitFor();
    eq(await page.locator('.yh-season.is-selected').getAttribute('data-season'), '2');
    eq(await page.locator('.yh-detail-actions button').first().innerText(), '返回结果');
    await page.locator('.detail-back button').click();
    eq(await page.evaluate(() => S.view), 'tasks');
  });
  await run('feedback draft/counts survive fixture failure and clear only on success', async () => {
    await page.goto(base + '?start_param=issues' + authHash);
    await page.locator('#issue-title').fill('字幕测试');
    await page.locator('#issue-description').fill('测试电影字幕不同步');
    eq(await page.locator('#issue-title-count').innerText(), '4 / 80');
    issueFailure = true;
    const errorStart = errors.length;
    await page.locator('.aux-form button[type="submit"]').click();
    await page.locator('.aux-form [role="alert"]').waitFor();
    eq(await page.locator('#issue-title').inputValue(), '字幕测试');
    eq(await page.locator('#issue-description').inputValue(), '测试电影字幕不同步');
    eq(await page.locator('.aux-form button[type="submit"]').isDisabled(), false);
    report.expected_fixture_http_errors = errors.splice(errorStart);
    assert.ok(report.expected_fixture_http_errors.every(e => e.type === 'console' && /503/.test(e.message)));
    await snapshot('feedback-retry');
    issueFailure = false;
    await page.locator('.aux-form button[type="submit"]').click();
    await page.waitForFunction(() => document.querySelector('#issue-title').value === '');
    const posts = requests.filter(x => x.path.endsWith('/issues') && x.method === 'POST');
    eq(posts.length, 2);
    eq(posts[0].body, posts[1].body);
    eq(posts[0].body.title, '字幕测试');
  });
  await run('real SDK bootstrap waits; detail deep link and native Back preserve route', async () => {
    sdkDelay = 300;
    const start = requests.length;
    await page.goto(base + '?start_param=detail_103_tv_2' + authHash, {waitUntil: 'domcontentloaded'});
    eq(requests.length, start, 'no API fetch before delayed SDK resolves');
    await page.locator('.yh-season.is-selected').waitFor();
    eq(await page.locator('.yh-season.is-selected').getAttribute('data-season'), '2');
    eq(requests.slice(start).filter(x => x.path.endsWith('/detail')).length, 1);
    eq(requests.slice(start).filter(x => x.path.endsWith('/detail'))[0].query, {id: '103', type: 'tv', season: '2'});
    assert.ok(requests.slice(start).every(x => x.auth));
    await snapshot('deeplink-season');
    await page.evaluate(() => window.Telegram.WebView.receiveEvent('back_button_pressed'));
    await page.locator('.studio-brand').waitFor();
    eq(await page.evaluate(() => S.detailVisible), false);
    sdkDelay = 0;
  });
  await run('unsigned query identity is not sent as authentication', async () => {
    const errorStart = errors.length;
    const authPage = await browser.newPage({viewport: report.viewport});
    authPage.on('pageerror', e => errors.push({type: 'pageerror', message: e.message}));
    await authPage.goto(base + '?start_param=search&initData=forged');
    await authPage.locator('#q').fill('auth fixture');
    await authPage.locator('#q').press('Enter');
    await authPage.locator('.yh-empty[role="alert"]').waitFor();
    assert.match(await authPage.locator('.yh-empty[role="alert"]').innerText(), /会话已过期/);
    eq(requests.at(-1).auth, false);
    eq(await authPage.locator('.yh-result').count(), 0);
    eq(errors.length, errorStart);
    await authPage.close();
  });
  eq(errors.length, 0, 'no unexpected JavaScript/console errors');
  assert.ok(!requests.some(x => ['/request', '/wash'].some(p => x.path.endsWith(p)) && x.method !== 'GET'), 'no request/wash submissions');
  report.pass = true;
})().catch(error => {report.pass = false; report.failure = error.stack; process.exitCode = 1;})
.finally(async () => {
  report.requests = requests;
  fs.writeFileSync(path.join(out, 'browser-report.json'), JSON.stringify(report, null, 2));
  if (browser) await browser.close();
  await new Promise(resolve => server.close(resolve));
});
