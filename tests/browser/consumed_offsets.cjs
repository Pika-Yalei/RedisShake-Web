const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const { chromium } = require('playwright');

async function main() {
  const base = process.env.TEST_BASE_URL || 'http://127.0.0.1:8080';
  const output = path.resolve(__dirname, '../../.redis-shake-web/dev/offset-ui');
  await fs.mkdir(output, { recursive: true });
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const page = await context.newPage();
  const errors = [], requested = [];
  let fail = false, offset = '9007199254740993';
  const tasks = Array.from({ length: 23 }, (_, i) => ({ id: `t${i + 1}`, name: `Redis Sync ${i + 1}`, sourceId: 'source', targetId: 'target' }));
  page.on('pageerror', error => errors.push(error.message));
  await context.route('**/api/**', async route => {
    const url = new URL(route.request().url()).pathname;
    const reply = json => route.fulfill({ json });
    if (url === '/api/bootstrap/status') return reply({ initialized: true });
    if (url === '/api/me') return reply({ username: 'fixture', csrf: 'fixture' });
    if (url === '/api/connections') return reply([]);
    if (url === '/api/tasks') return reply(tasks);
    const match = url.match(/^\/api\/tasks\/(t\d+)\/runs$/);
    if (match) {
      requested.push(match[1]);
      if (fail) return route.fulfill({ status: 503, json: { error: '测试读取失败' } });
      const positions = match[1] === 't1' ? [{ node: 'source:6379', offset }] : match[1] === 't2' ? [{ node: 'node-a:6379', offset: '0' }, { node: 'node-b:6379', offset: '9223372036854775807' }] : [];
      return reply([{ id: 'run-' + match[1], status: 'RUNNING', consumedOffsets: positions }]);
    }
    errors.push('Unexpected API: ' + url);
    return route.fulfill({ status: 500, json: { error: url } });
  });
  try {
    await page.clock.install();
    await page.goto(base + '/tasks');
    const cell = id => page.locator(`[data-run-offset="${id}"]`);
    await cell('t1').getByText(offset, { exact: true }).waitFor();
    assert.deepEqual(await page.locator('thead th').allTextContents(), ['任务名称', '运行状态', '消费位点（Offset）', '操作']);
    assert.equal(await cell('t3').innerText(), '—');
    assert.match(await cell('t2').innerText(), /node-a:6379\n0\nnode-b:6379\n9223372036854775807/);
    const fullBounds = await page.locator('.table-scroll').boundingBox();
    await page.getByRole('searchbox').fill('Redis Sync');
    await cell('t1').getByText(offset, { exact: true }).waitFor();
    offset = '9007199254741089'; requested.length = 0;
    await page.clock.fastForward(5100);
    await cell('t1').getByText(offset, { exact: true }).waitFor();
    assert.equal(await page.getByRole('searchbox').inputValue(), 'Redis Sync');
    assert.equal(await page.getByRole('searchbox').evaluate(el => el === document.activeElement), true);
    assert.equal(new Set(requested).size, 10);
    assert(requested.every(id => Number(id.slice(1)) <= 10));
    await page.screenshot({ path: path.join(output, 'offsets-1440.png') });
    await page.getByRole('button', { name: '下一页', exact: true }).click();
    await cell('t11').waitFor();
    await page.locator('[data-run-status="t20"]').getByText('运行中', { exact: true }).waitFor();
    requested.length = 0;
    await page.clock.fastForward(5100);
    await page.waitForFunction(() => document.querySelector('[data-run-status="t20"]').textContent === '运行中');
    assert.equal(new Set(requested).size, 10);
    assert(requested.every(id => Number(id.slice(1)) >= 11 && Number(id.slice(1)) <= 20));
    assert.match(await page.locator('.pagination-pages').innerText(), /第 2 \/ 3 页/);
    fail = true;
    await page.clock.fastForward(5100);
    await cell('t11').getByText('读取失败', { exact: true }).waitFor();
    fail = false;
    await page.clock.fastForward(5100);
    await cell('t11').getByText('—', { exact: true }).waitFor();
    await page.getByRole('searchbox').fill('no matching tasks');
    await page.getByText('未找到匹配的任务', { exact: true }).waitFor();
    assert.equal(await page.locator('.table-scroll').evaluate(el => Math.round(el.getBoundingClientRect().height)), Math.round(fullBounds.height));
    assert.equal(await page.locator('thead th').count(), 4);
    assert.equal(await page.locator('.table-empty').getAttribute('colspan'), '4');
    requested.length = 0;
    await page.clock.fastForward(5100);
    assert.equal(requested.length, 0);
    await page.getByRole('searchbox').fill('');
    await cell('t1').getByText(offset, { exact: true }).waitFor();
    await page.setViewportSize({ width: 1100, height: 800 });
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    await page.screenshot({ path: path.join(output, 'offsets-1100.png') });
    await page.locator('#nav-connections').click();
    await page.getByRole('heading', { name: '连接管理', exact: true }).waitFor();
    requested.length = 0;
    await page.clock.fastForward(5100);
    assert.equal(requested.length, 0);
    assert.deepEqual(errors, []);
    console.log('PASS: offset strings, zero/unknown/cluster positions, current-page polling, search/focus/pagination preserved, error recovery, fixed empty table, desktop bounds, polling cleanup. API fixtures only.');
  } finally {
    await context.close(); await browser.close();
  }
}
main().catch(error => { console.error(error); process.exitCode = 1; });
