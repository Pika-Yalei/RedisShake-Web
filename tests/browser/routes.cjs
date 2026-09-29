const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const { chromium } = require('playwright');

const baseURL = process.env.TEST_BASE_URL || 'http://127.0.0.1:8080';
const output = path.resolve(__dirname, '../../.redis-shake-web/dev/routes');

async function main() {
  await fs.mkdir(output, { recursive: true });
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const errors = [];
  const unexpected = [];
  let authenticated = true;
  let initialized = true;
  let failLists = false;
  let delayLists = false;
  let releaseLists;
  let listsRequested;
  let runsRequested;
  let releaseRuns;
  let delayRuns = false;
  let createdTasks = 0;
  const previews = [];
  let failStart = false;
  const connections = Array.from({ length: 23 }, (_, i) => ({ id: `c${i + 1}`, name: `Redis Connection ${i + 1}`, kind: 'standalone', address: `localhost:${6379 + i}`, authMode: 'none' }));
  const tasks = Array.from({ length: 23 }, (_, i) => ({ id: `t${i + 1}`, name: `Redis Sync ${i + 1}`, sourceId: 'c1', targetId: 'c2', dbMap: { '0': 0 }, rules: {}, targetPolicy: 'require_empty', updatedAt: '2026-09-28T12:00:00Z' }));
  context.on('page', page => page.on('pageerror', error => errors.push(error.message)));
  await context.route('**/api/**', async route => {
    const req = route.request();
    const url = new URL(req.url()).pathname;
    const method = req.method();
    const reply = (body, status = 200) => route.fulfill({ status, json: body });
    if (url === '/api/bootstrap/status') return reply({ initialized });
    if (url === '/api/bootstrap/init') { initialized = true; return reply({ ok: true }); }
    if (url === '/api/login') { authenticated = true; return reply({ username: 'admin', csrf: 'fixture' }); }
    if (!authenticated) return reply({ error: '请登录' }, 401);
    if (url === '/api/me') return reply({ username: 'admin', csrf: 'fixture' });
    if (method === 'GET' && ['/api/connections', '/api/tasks'].includes(url)) {
      const body = structuredClone(url === '/api/connections' ? connections : tasks);
      if (failLists) return reply({ error: '测试读取失败' }, 503);
      if (delayLists) { listsRequested(); await new Promise(resolve => { releaseLists = resolve; }); }
      return reply(body);
    }
    if (method === 'GET' && /^\/api\/tasks\/[^/]+\/runs$/.test(url)) {
      if (delayRuns && url === '/api/tasks/t1/runs') {
        delayRuns = false; runsRequested();
        await new Promise(resolve => { releaseRuns = resolve; });
        return reply([{ id: 'old-run', status: 'FAILED', error: 'STALE TASK RESPONSE', startedAt: '2026-09-28' }]);
      }
      if (url === '/api/tasks/t11/runs') return reply([{ id: 'active-fixture', status: 'RUNNING', phase: 'INCREMENTAL', startedAt: '2026-09-29' }]);
      return reply([]);
    }
    if (url === '/api/runs/active-fixture/logs') return reply({ text: 'Fixture log' });
    if (method === 'POST' && url === '/api/connections') {
      const item = { ...req.postDataJSON(), id: 'saved-connection' };
      connections.push(item); return reply(item, 201);
    }
    if (method === 'PUT' && url.startsWith('/api/connections/')) {
      const item = connections.find(item => item.id === url.split('/').pop());
      Object.assign(item, req.postDataJSON()); return reply(item);
    }
    if (method === 'POST' && url === '/api/tasks') {
      const item = { ...req.postDataJSON(), id: 'saved-task' };
      tasks.push(item); createdTasks++; return reply(item, 201);
    }
    if (method === 'POST' && url === '/api/tasks/preflight') previews.push(req.postDataJSON());
    if (method === 'POST' && url.endsWith('/preflight')) return reply([{ name: '预检', ok: true, message: '通过' }]);
    if (method === 'POST' && url.endsWith('/start')) return failStart ? reply({ error: '目标端预检未通过' }, 409) : reply({ id: 'fixture-run' });
    if (method === 'DELETE' && url.startsWith('/api/tasks/')) {
      tasks.splice(tasks.findIndex(item => item.id === url.split('/').pop()), 1); return reply({ ok: true });
    }
    unexpected.push(`${method} ${url}`);
    return reply({ error: 'Unexpected mock API request' }, 500);
  });
  const page = await context.newPage();
  const heading = text => page.getByRole('heading', { name: text, exact: true }).waitFor();
  const at = async (url, title) => {
    await page.waitForURL(baseURL + url);
    await heading(title);
    assert.equal(await page.title(), title + ' · RedisShake Web');
  };
  try {
    for (const [url, title] of [
      ['/tasks', '同步任务'], ['/connections', '连接管理'],
      ['/tasks/new', '同步任务 / 新建任务'], ['/tasks/t1', '同步任务 / Redis Sync 1'],
      ['/connections/new', '连接管理 / 新建连接'],
      ['/connections/c1/edit', '连接管理 / 编辑连接'],
    ]) {
      assert.equal((await page.goto(baseURL + url)).status(), 200);
      await at(url, title);
      await page.reload(); await at(url, title);
    }
    assert.equal(await page.locator('#name').inputValue(), 'Redis Connection 1');
    await page.locator('.title-parent').focus(); await page.keyboard.press('Space');
    await at('/connections', '连接管理');
    await page.getByRole('searchbox').fill('Redis Connection');
    await page.getByRole('button', { name: '下一页', exact: true }).click();
    await page.locator('[data-edit-connection="c11"]').click();
    await at('/connections/c11/edit', '连接管理 / 编辑连接');
    await page.goBack(); await at('/connections', '连接管理');
    assert.equal(await page.getByRole('searchbox').inputValue(), 'Redis Connection');
    assert.match(await page.locator('.pagination-pages').innerText(), /第 2 \/ 3 页/);
    await page.goForward(); await at('/connections/c11/edit', '连接管理 / 编辑连接');
    await page.locator('#name').fill('Updated fixture');
    await page.getByRole('button', { name: '保存连接', exact: true }).click();
    await at('/connections', '连接管理');
    await page.locator('#new-connection').click();
    await at('/connections/new', '连接管理 / 新建连接');
    await page.locator('#address').fill('127.0.0.1:6380');
    await page.getByRole('button', { name: '保存连接', exact: true }).click();
    await at('/connections', '连接管理');
    const popupReady = context.waitForEvent('page');
    await page.locator('#new-connection').click({ modifiers: ['ControlOrMeta'] });
    const popup = await popupReady;
    await popup.getByRole('heading', { name: '连接管理 / 新建连接', exact: true }).waitFor();
    assert.equal(popup.url(), baseURL + '/connections/new'); await popup.close();
    await page.locator('#nav-tasks').click(); await at('/tasks', '同步任务');
    await page.getByRole('searchbox').fill('Redis Sync');
    await page.getByRole('button', { name: '下一页', exact: true }).click();
    await page.locator('[data-task="t11"]').click();
    await at('/tasks/t11', '同步任务 / Redis Sync 11');
    await page.getByText('运行中', { exact: true }).waitFor();
    assert.equal(await page.getByRole('button', { name: '编辑', exact: true }).count(), 0);
    assert.equal(await page.getByText('目标 DB 清理', { exact: true }).count(), 0);
    assert.equal(await page.locator('#run-start').isDisabled(), true);
    await page.screenshot({ path: path.join(output, 'task-detail-desktop.png') });
    await page.locator('#back-tasks').click(); await at('/tasks', '同步任务');
    assert.match(await page.locator('.pagination-pages').innerText(), /第 2 \/ 3 页/);
    await page.locator('#new-task').click(); await at('/tasks/new', '同步任务 / 新建任务');
    await page.locator('#name').fill('Saved URL task');
    for (const [id, label] of [['sourceId', 'Redis Connection 1 · standalone'], ['targetId', 'Redis Connection 2 · standalone']]) {
      await page.locator(`#${id}-trigger`).click();
      await page.getByRole('option', { name: label, exact: true }).click();
    }
    await page.locator('#next-step').click(); await page.locator('#dbMap').waitFor();
    await page.locator('#next-step').click(); await at('/tasks/new', '同步任务 / 新建任务');
    await page.getByRole('button', { name: '重新预检', exact: true }).waitFor();
    assert.equal(createdTasks, 0);
    await page.locator('#previous-step').click(); await page.locator('#dbMap').waitFor();
    await page.locator('#previous-step').click(); await page.locator('#name').fill('Saved URL task updated');
    await page.locator('#next-step').click(); await page.locator('#dbMap').waitFor();
    await page.locator('#next-step').click(); await page.locator('#rerun-checks').waitFor();
    assert.equal(createdTasks, 0);
    assert.equal(previews.at(-1).name, 'Saved URL task updated');
    await page.locator('#next-step').click(); await page.locator('#start-sync').click();
    await at('/tasks/saved-task', '同步任务 / Saved URL task updated');
    assert.equal(createdTasks, 1);
    await page.reload(); await at('/tasks/saved-task', '同步任务 / Saved URL task updated');
    page.once('dialog', dialog => dialog.accept());
    await page.locator('#run-delete').click(); await at('/tasks', '同步任务');
    await page.goBack(); await at('/tasks/saved-task', '同步任务 / 页面不存在');
    // A failed start keeps the created task available for retry without opening
    // an editing flow or creating another task on the next attempt.
    await page.goto(baseURL + '/tasks/new'); await heading('同步任务 / 新建任务');
    await page.locator('#name').fill('Retry startup');
    for (const [id, label] of [['sourceId', 'Redis Connection 1 · standalone'], ['targetId', 'Redis Connection 2 · standalone']]) {
      await page.locator(`#${id}-trigger`).click();
      await page.getByRole('option', { name: label, exact: true }).click();
    }
    await page.locator('#next-step').click(); await page.locator('#dbMap').waitFor();
    await page.locator('#next-step').click(); await page.locator('#rerun-checks').waitFor();
    await page.locator('#next-step').click(); failStart = true;
    await page.locator('#start-sync').click(); await at('/tasks/saved-task', '同步任务 / Retry startup');
    await page.getByText('目标端预检未通过', { exact: true }).waitFor();
    assert.equal(createdTasks, 2);
    failStart = false;
    await page.locator('#run-start').click();
    await page.getByText('同步已启动', { exact: true }).waitFor();
    assert.equal(createdTasks, 2);
    page.once('dialog', dialog => dialog.accept());
    await page.locator('#run-delete').click(); await at('/tasks', '同步任务');
    await page.goto(baseURL + '/connections/removed/edit');
    await heading('连接管理 / 页面不存在');

    // Slow navigation must not overwrite a newer page.
    await page.goto(baseURL + '/tasks'); await heading('同步任务');
    const pendingLists = new Promise(resolve => { listsRequested = resolve; });
    delayLists = true;
    // Hold only one list request, allowing the other to complete normally.
    const originalCallback = listsRequested;
    listsRequested = () => { delayLists = false; originalCallback(); };
    await page.locator('#nav-connections').click(); await pendingLists;
    await page.locator('#nav-tasks').click(); await at('/tasks', '同步任务');
    releaseLists();
    await page.getByRole('table', { name: '同步任务列表' }).waitFor();
    assert.equal(page.url(), baseURL + '/tasks');

    const pendingRuns = new Promise(resolve => { runsRequested = resolve; });
    delayRuns = true;
    await page.locator('[data-task="t1"]').click(); await pendingRuns;
    await page.locator('#nav-tasks').click(); await heading('同步任务');
    await page.locator('[data-task="t2"]').click(); await at('/tasks/t2', '同步任务 / Redis Sync 2');
    delayRuns = false; releaseRuns();
    await page.getByText('尚未运行', { exact: true }).waitFor();
    assert.equal(await page.getByText('STALE TASK RESPONSE').count(), 0);

    failLists = true;
    await page.locator('#nav-connections').click(); await heading('连接管理 / 加载失败');
    assert.equal(page.url(), baseURL + '/connections');
    failLists = false;
    await page.getByRole('button', { name: '重试', exact: true }).click(); await at('/connections', '连接管理');
    authenticated = false;
    await page.goto(baseURL + '/tasks/t2');
    await page.getByRole('button', { name: '登录', exact: true }).waitFor();
    assert.equal(page.url(), baseURL + '/tasks/t2');
    await page.locator('#password').fill('fixture');
    await page.getByRole('button', { name: '登录', exact: true }).click();
    await at('/tasks/t2', '同步任务 / Redis Sync 2');
    assert.equal(await page.getByRole('button', { name: '编辑', exact: true }).count(), 0);
    authenticated = false; initialized = false;
    await page.goto(baseURL + '/connections/new'); await heading('初始化管理员账号');
    await page.getByRole('button', { name: '创建管理员账号', exact: true }).click();
    await page.getByRole('button', { name: '登录', exact: true }).waitFor();
    await page.locator('#password').fill('fixture'); await page.getByRole('button', { name: '登录', exact: true }).click();
    await at('/connections/new', '连接管理 / 新建连接');
    await page.goto(baseURL + '/'); await at('/tasks', '同步任务');
    await page.goto(baseURL + '/?from=bookmark'); await at('/tasks?from=bookmark', '同步任务');
    assert.equal(await page.locator('thead .table-actions').evaluate(el => getComputedStyle(el).textAlign), 'left');
    assert.equal(await page.locator('[data-task]').first().innerText(), '详情');
    await page.screenshot({ path: path.join(output, 'tasks-desktop.png') });
    await page.locator('#nav-connections').click(); await heading('连接管理');
    const search = await page.getByRole('searchbox').boundingBox();
    const create = await page.locator('#new-connection').boundingBox();
    assert.equal(search.height, create.height); assert.equal(search.y, create.y);
    assert.equal(await page.locator('thead .table-actions').evaluate(el => getComputedStyle(el).textAlign), 'left');
    await page.screenshot({ path: path.join(output, 'connections-desktop.png') });
    assert.deepEqual(errors, []); assert.deepEqual(unexpected, []);
    console.log('PASS: direct URLs, reload, history, links, auth return, unsaved preview, create/start and failed-start retry, removed task controls, left action headings, pagination, missing records, request races, desktop layout. All API calls mocked.');
  } finally {
    releaseLists?.(); releaseRuns?.();
    await context.close(); await browser.close();
  }
}

main().catch(error => { console.error(error); process.exitCode = 1; });
