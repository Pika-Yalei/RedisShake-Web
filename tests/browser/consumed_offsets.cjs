const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const { chromium } = require('playwright');

async function main() {
  const base = process.env.TEST_BASE_URL || 'http://127.0.0.1:8080';
  const output = path.resolve(__dirname, '../../.redis-shake-web/dev/offset-ui');
  await fs.mkdir(output, { recursive: true });
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'zh-CN', reducedMotion: 'reduce' });
  const page = await context.newPage();
  const errors = [], requested = [];
  const now = new Date('2026-09-29T04:00:00Z');
  let fail = false, failedNode = false, stale = false, status = 'RUNNING';
  let offset = '9007199254740993', lag = '100';
  const tasks = Array.from({ length: 23 }, (_, i) => ({ id: `t${i + 1}`, name: `Redis Sync ${i + 1}`, sourceId: 'source', targetId: 'target' }));
  page.on('pageerror', error => errors.push(error.message));
  await context.route('**/api/**', async route => {
    const url = new URL(route.request().url()).pathname;
    const reply = json => route.fulfill({ json });
    if (url === '/api/bootstrap/status') return reply({ initialized: true });
    if (url === '/api/me') return reply({ username: 'fixture', csrf: 'fixture' });
    if (url === '/api/connections') return reply([]);
    if (url === '/api/tasks') return reply(tasks);
    if (/^\/api\/runs\/[^/]+\/logs$/.test(url)) return reply({ text: 'Fixture logs' });
    const match = url.match(/^\/api\/tasks\/(t\d+)\/runs$/);
    if (match) {
      requested.push(match[1]);
      if (fail) return route.fulfill({ status: 503, json: { error: '测试读取失败' } });
      if (match[1] === 't3') return reply([]);
      // Keep the fixture timestamp deterministic while polling timers advance.
      const sampledAt = new Date(now.getTime() - (stale ? 60000 : 0)).toISOString();
      const nodeProgress = match[1] === 't1' ? [
        { node: 'redis-source-a:6379', offset, masterNode: 'redis-master-a:6379', masterOffset: '9007199254741093', lag: failedNode ? null : lag, sampledAt, error: failedNode ? 'master 连接失败' : '' },
        { node: 'redis-source-b:6379', offset: '0', masterOffset: '0', lag: '0', sampledAt },
        { node: 'redis-source-c:6379', offset: null, masterOffset: '9223372036854775807', lag: null, sampledAt },
      ] : [];
      return reply([{ id: 'run-' + match[1], status, phase: 'INCREMENTAL', startedAt: now.toISOString(), nodeProgress,
        consumedOffsets: match[1] === 't2' ? [{ node: 'legacy-node:6379', offset: '9223372036854775807' }] : [] }]);
    }
    errors.push('Unexpected API: ' + url);
    return route.fulfill({ status: 500, json: { error: url } });
  });
  const row = node => page.locator('#run-progress tbody tr').filter({ has: page.getByRole('rowheader', { name: node, exact: node !== 'redis-source-a:6379' }) });
  const refresh = async () => { await page.getByRole('button', { name: '刷新', exact: true }).click(); };
  try {
    await page.clock.install({ time: now });
    await page.goto(base + '/tasks');
    await page.locator('[data-run-status="t10"]').getByText('运行中', { exact: true }).waitFor();
    assert.deepEqual(await page.locator('thead th').allTextContents(), ['任务名称', '运行状态', '操作']);
    const fullBounds = await page.locator('.table-scroll').boundingBox();
    await page.getByRole('searchbox').fill('Redis Sync');
    await page.locator('[data-run-status="t10"]').getByText('运行中', { exact: true }).waitFor();
    requested.length = 0;
    await Promise.all([page.waitForResponse(r => r.url().endsWith('/tasks/t10/runs')),page.clock.fastForward(5100)]);
    assert.equal(await page.getByRole('searchbox').inputValue(), 'Redis Sync');
    assert(await page.getByRole('searchbox').evaluate(el => el === document.activeElement));
    assert.equal(new Set(requested).size, 10);
    assert(requested.every(id => Number(id.slice(1)) <= 10));
    await page.getByRole('button', { name: '下一页', exact: true }).click();
    await page.locator('[data-run-status="t20"]').getByText('运行中', { exact: true }).waitFor();
    requested.length = 0;
    fail = true;
    await page.clock.fastForward(5100);
    await page.locator('[data-run-status="t11"]').getByText('读取失败', { exact: true }).waitFor();
    assert(requested.every(id => Number(id.slice(1)) >= 11 && Number(id.slice(1)) <= 20));
    assert.match(await page.locator('.pagination-pages').innerText(), /第 2 \/ 3 页/);
    fail = false;
    await page.getByRole('searchbox').fill('no matching tasks');
    await page.getByText('未找到匹配的任务', { exact: true }).waitFor();
    assert.equal(Math.round((await page.locator('.table-scroll').boundingBox()).height), Math.round(fullBounds.height));
    assert.equal(await page.locator('.table-empty').getAttribute('colspan'), '3');
    await page.getByRole('searchbox').fill('');
    await page.clock.setFixedTime(now);
    await page.locator('[data-task="t1"]').click();
    await row('redis-source-a:6379').getByText(offset, { exact: true }).waitFor();
    assert.deepEqual(await page.locator('#run-progress thead th').allTextContents(), ['源节点', '消费 Offset', '延迟 Offset', '上报状态']);
    assert.equal(await row('redis-source-a:6379').locator('td').nth(1).innerText(), '100');
    assert.equal(await row('redis-source-b:6379').locator('td').nth(0).innerText(), '0');
    assert.equal(await row('redis-source-b:6379').locator('td').nth(1).innerText(), '0');
    assert.equal(await row('redis-source-c:6379').locator('td').nth(0).innerText(), '—');
    assert.equal(await row('redis-source-c:6379').locator('td').nth(1).innerText(), '—');
    await page.clock.runFor(500);
    await page.waitForFunction(()=>[...document.querySelectorAll('.card')].every(el=>getComputedStyle(el).opacity==='1'));
    await page.screenshot({ animations: 'disabled', path: path.join(output, 'node-progress-1440.png') });
    offset = '9007199254741089'; lag = '73'; // Display the task's value; no browser subtraction.
    await page.clock.fastForward(4100);
    await row('redis-source-a:6379').getByText(offset, { exact: true }).waitFor();
    assert.equal(await row('redis-source-a:6379').locator('td').nth(1).innerText(), '73');
    failedNode = true; await refresh();
    await row('redis-source-a:6379').getByText('master 连接失败', { exact: true }).waitFor();
    assert.equal(await row('redis-source-a:6379').locator('td').nth(1).innerText(), '—');
    assert.equal(await row('redis-source-b:6379').locator('td').nth(1).innerText(), '0');
    failedNode = false; stale = true; await refresh();
    await row('redis-source-a:6379').getByText('上报已过期', { exact: true }).waitFor();
    assert.equal(await row('redis-source-a:6379').locator('td').nth(1).innerText(), '—');
    status = 'STOPPED'; await refresh();
    await row('redis-source-a:6379').getByText('最后上报', { exact: true }).waitFor();
    assert.equal(await row('redis-source-a:6379').locator('td').nth(1).innerText(), '73');
    await page.setViewportSize({ width: 1100, height: 800 });
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    await page.getByRole('heading', { name: '同步位点', exact: true }).scrollIntoViewIfNeeded();
    await page.screenshot({ animations: 'disabled', path: path.join(output, 'node-progress-1100.png') });
    const scroller=page.locator('#run-progress .table-scroll');
    assert(await scroller.evaluate(el=>el.scrollWidth>el.clientWidth));
    await scroller.focus();
    for(let i=0;i<20;i++)await page.keyboard.press('ArrowRight');
    await page.waitForFunction(()=>{const el=document.querySelector('#run-progress .table-scroll');return el.scrollLeft===el.scrollWidth-el.clientWidth;});
    const left=await scroller.evaluate(el=>el.scrollLeft);assert(left>0);
    await scroller.focus();
    await Promise.all([page.waitForResponse(r=>r.url().endsWith('/tasks/t1/runs')),page.clock.fastForward(4100)]);
    await page.clock.runFor(100);
    assert.equal(await scroller.evaluate(el=>el.scrollLeft),left);
    assert(await scroller.evaluate(el=>el===document.activeElement));
    fail = true; await refresh();
    await page.getByText('位点读取失败：测试读取失败', { exact: true }).waitFor();
    assert.equal(await page.locator('#run-progress thead th').count(), 4);
    fail = false; await refresh();
    await row('redis-source-a:6379').getByText(offset, { exact: true }).waitFor();
    await page.locator('#back-tasks').click();
    await page.locator('[data-task="t2"]').click();
    await row('legacy-node:6379').getByText('9223372036854775807', { exact: true }).waitFor();
    assert.equal(await row('legacy-node:6379').locator('td').nth(1).innerText(), '—');
    await page.locator('#back-tasks').click();
    await page.locator('[data-task="t3"]').click();
    await page.getByText('尚未运行，暂无位点', { exact: true }).waitFor();
    await page.locator('#nav-connections').click();
    await page.getByRole('heading', { name: '连接管理', exact: true }).waitFor();
    requested.length = 0;
    await page.clock.fastForward(5100);
    assert.equal(requested.length, 0);
    assert.deepEqual(errors, []);
    console.log('PASS: per-node reported offsets/lag, bigint/zero/unknown, failure/stale/history, three-column list, paging/focus, error recovery, desktop bounds, navigation cleanup. API fixtures only.');
  } finally {
    await context.close(); await browser.close();
  }
}
main().catch(error => { console.error(error); process.exitCode = 1; });
