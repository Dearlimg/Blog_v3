import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { readFile, mkdir } from 'node:fs/promises';
import { chromium } from 'playwright';

const answer = '## Linux 内存管理\n\n**先看全貌**，再理解 `VMA` 与物理页。\n\n| 概念 | 作用 |\n| --- | --- |\n| VMA | 描述虚拟地址区间 |\n| Page | 管理物理内存页 |\n\n### 排查步骤\n\n1. 检查进程内存\n2. 定位页缓存\n\n```go\nfmt.Println("hello")\n```\n\n> 先观察，再定位。\n\n[官方资料](https://example.com/docs)\n\n<script>window.injected = true</script><img src=x onerror="window.injected=true">[危险链接](javascript:alert(1))';
const created = '2026-11-01T08:00:00Z';
const event = (type, rest = {}) => ({ type, text: '', at: created, ...rest });
const initial = [
  event('model_start', { message_id: 'm1', agent: 'lead' }),
  event('assistant_delta', { message_id: 'm1', text: '我先检索官方资料，再整理关键概念。' }),
  event('model_end', { message_id: 'm1', status: 'completed', duration_ms: 380 }),
  event('tool_start', { tool: 'web_search', call_id: 't1', arguments: '{"query":"Linux memory management"}' }),
  event('tool_end', { tool: 'web_search', call_id: 't1', status: 'completed', result: '[{"title":"Linux docs","url":"https://example.com/docs"}]', duration_ms: 420 }),
  event('tool_start', { tool: 'web_read', call_id: 't2', arguments: '{"url":"https://example.com/docs"}' }),
  event('tool_end', { tool: 'web_read', call_id: 't2', status: 'failed', result: '{"error":"请求超时"}', duration_ms: 1300 }),
  event('model_start', { message_id: 'm2', agent: 'lead' }),
  event('assistant_delta', { message_id: 'm2', text: '## Linux 内存管理\n\n' }),
];

async function fixture(t, mode = 'normal') {
  let release;
  const gate = new Promise(resolve => { release = resolve; });
  let completed = false, cancelled = false;
  const queued = [], queueEvents = [], queueRequests = [];
  let emitActivity;
  const cursors = [], auth = [], errors = [];
  const run = () => ({ id: 'run', prompt: '讲解 Linux 内存管理', status: completed ? cancelled ? 'cancelled' : 'completed' : 'running', answer: completed && !cancelled ? answer : '', error: cancelled ? 'context canceled' : '', events: completed ? allEvents() : [...initial, ...queueEvents], inputs: queued.map(item => ({ ...item, status: completed && item.status === 'pending' ? 'unprocessed' : item.status })), sources: [{ title: 'Linux 官方文档', url: 'https://example.com/docs' }], calls: 2, created_at: created, updated_at: '2026-11-01T08:00:04Z' });
  function allEvents() {
    return [...initial, ...queueEvents, event('assistant_delta', { message_id: 'm2', text: answer.slice('## Linux 内存管理\n\n'.length) }), event('model_end', { message_id: 'm2', status: 'completed', duration_ms: 1800 })];
  }
  const server = createServer(async (req, res) => {
    const url = new URL(req.url, 'http://localhost');
    const json = (data, status = 200) => { res.writeHead(status, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(data)); };
    if (url.pathname === '/api/v1/agent/info') return json({ configured: true, owner: req.headers.authorization === 'Bearer valid' });
    if (url.pathname === '/api/v1/agent/owner/state') return json({ jobs: [] });
    if (url.pathname === '/api/v1/agent/sessions' && req.method === 'POST') return json({ id: 'session' });
    if (url.pathname.endsWith('/messages')) return json({ run_id: 'run' });
    if (url.pathname.endsWith('/inputs') && req.method === 'POST') {
      let body = ''; for await (const chunk of req) body += chunk;
      const input = JSON.parse(body); queueRequests.push(input);
      let item = queued.find(item => item.id === input.id);
      if (!item) {
        item = { id: input.id, content: input.message, mode: input.mode, status: 'pending' }; queued.push(item);
        const activity = event('input_queued', { input_id: item.id, mode: item.mode, status: 'pending', text: item.content });
        queueEvents.push(activity); await emitActivity(activity);
      }
      return json(item, 202);
    }
    if (url.pathname.includes('/inputs/') && req.method === 'DELETE') {
      const item = queued.find(item => item.id === url.pathname.split('/').at(-1)); item.status = 'removed';
      const activity = event('input_removed', { input_id: item.id, mode: item.mode, status: 'removed', text: item.content });
      queueEvents.push(activity); await emitActivity(activity); return json({ status: 'removed' });
    }
    if (url.pathname.endsWith('/cancel')) { cancelled = true; completed = true; release(); return json({ status: 'cancellation_requested' }); }
    if (url.pathname === '/api/v1/agent/runs/run') return json(run());
    if (url.pathname === '/api/v1/agent/sessions/session') return json({ id: 'session', messages: [], active_run: '', approvals: [], todos: [], runs: completed ? [run()] : [] });
    if (url.pathname === '/api/v1/agent/runs/run/events') {
      const cursor = Number(url.searchParams.get('after') || 0); cursors.push(cursor); auth.push(req.headers.authorization || '');
      res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' }); res.flushHeaders();
      const send = async (kind, data, id) => {
        const text = (id ? `id: ${id}\n` : '') + `event: ${kind}\ndata: ${JSON.stringify(data)}\n\n`;
        // Split UTF-8 and SSE frames at arbitrary transport boundaries.
        const bytes = Buffer.from(text);
        for (let start = 0; start < bytes.length; start += 13) res.write(bytes.subarray(start, start + 13));
      };
      await send('run', { id: 'run', status: 'running' });
      for (let i = cursor; i < initial.length; i++) await send('activity', initial[i], i + 1);
      let sequence = initial.length;
      emitActivity = data => send('activity', data, ++sequence);
      if (mode === 'reconnect' && cursor === 0) { res.end(); return; }
      await gate;
      if (res.destroyed) return;
      completed = true;
      if (!cancelled) {
        for (let i = initial.length + queueEvents.length; i < allEvents().length; i++) await send('activity', allEvents()[i], i + 1);
      }
      await send('done', { ...run(), events: [] }); res.end(); return;
    }
    const files = {
      '/agent': ['view/html/agent.html', 'text/html'],
      '/static/agent-build/agent.js': ['static/agent-build/agent.js', 'text/javascript'],
      '/static/agent-build/agent.css': ['static/agent-build/agent.css', 'text/css'],
      '/view/css/agent.css': ['view/css/agent.css', 'text/css'],
      '/static/Ava.jpg': ['static/Ava.jpg', 'image/jpeg'],
    };
    if (!files[url.pathname]) { res.writeHead(404); res.end(); return; }
    const [file, type] = files[url.pathname]; res.writeHead(200, { 'Content-Type': type }); res.end(await readFile(file));
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  t.after(async () => { release(); server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); });
  const browser = await chromium.launch({ headless: true, ...(process.env.PLAYWRIGHT_CHANNEL ? { channel: process.env.PLAYWRIGHT_CHANNEL } : {}) });
  const page = await browser.newPage({ viewport: { width: 1365, height: 1000 } });
  page.on('pageerror', error => errors.push(error.message));
  await page.route('https://**', route => route.abort());
  t.after(async () => { release(); await browser.close(); });
  await page.goto(`http://127.0.0.1:${server.address().port}/agent`);
  if (mode === 'owner') {
    await page.locator('#owner-toggle').click();
    await page.locator('#owner-token').fill('valid');
    await page.locator('#owner-login button').click();
    await page.locator('#owner-tools').waitFor({ state: 'visible' });
    await page.locator('#owner-dialog .dialog-header button').click();
  }
  await page.locator('#prompt').fill('讲解 Linux 内存管理'); await page.locator('#send').click();
  await page.locator('.body h2').waitFor();
  return { page, release, cursors, auth, errors, queueRequests };
}

test('live SSE shows real tools and formatted text before completion; history retains trace', async t => {
  const { page, release, errors } = await fixture(t);
  assert.equal(await page.locator('#cancel').isVisible(), true);
  assert.equal(await page.locator('.tool-event').count(), 2);
  await page.locator('.tool-event').first().locator('summary').click();
  assert.match(await page.locator('.tool-input').first().innerText(), /Linux memory management/);
  assert.match(await page.locator('.tool-result').first().innerText(), /Linux docs/);
  assert.match(await page.locator('.tool-event[data-status=failed]').innerText(), /执行失败/);
  release();
  await page.waitForFunction(() => document.querySelector('#cancel').hidden);
  assert.equal(await page.locator('.body table tbody tr').count(), 2);
  assert.equal((await page.locator('.body pre code').innerText()).trimEnd(), 'fmt.Println("hello")');
  assert.equal(await page.locator('.copy-code').count(), 1);
  assert.equal(await page.locator('.body script, .body img, .body a[href^="javascript:"]').count(), 0);
  assert.equal(await page.evaluate(() => window.injected), undefined);
  assert.equal(await page.locator('.message.assistant').count(), 2);
  assert.equal(await page.locator('.sources a').count(), 1);
  assert.match(await page.locator('.run-meta').innerText(), /2 次工具调用/);
  await page.reload();
  await page.locator('#sessions button').click();
  await page.locator('.body table').waitFor();
  assert.equal(await page.locator('.tool-event').count(), 2);
  assert.equal(await page.locator('.message.user').count(), 1);
  assert.deepEqual(errors, []);
  if (process.env.AGENT_SCREENSHOT_DIR) {
    await mkdir(process.env.AGENT_SCREENSHOT_DIR, { recursive: true });
    await page.locator('#conversation').evaluate(el => { el.scrollTop = 0; });
    await page.locator('.tool-event').first().locator('summary').click();
    await page.screenshot({ path: `${process.env.AGENT_SCREENSHOT_DIR}/agent-desktop.png` });
    await page.setViewportSize({ width: 390, height: 844 });
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true);
    await page.screenshot({ path: `${process.env.AGENT_SCREENSHOT_DIR}/agent-mobile.png` });
  }
});

test('SSE reconnect resumes cursor without duplicating tool cards or text', async t => {
  const { page, release, cursors, auth, errors } = await fixture(t, 'reconnect');
  await page.waitForFunction(() => document.querySelector('#run-status').textContent.includes('重连'));
  await new Promise(resolve => setTimeout(resolve, 1400));
  assert.deepEqual(cursors, [0, initial.length]);
  release();
  await page.waitForFunction(() => document.querySelector('#cancel').hidden);
  assert.equal(await page.locator('.tool-event').count(), 2);
  assert.equal(await page.locator('.message.assistant').count(), 2);
  assert.equal(await page.locator('.body h2').count(), 1);
  assert.deepEqual(auth, ['', '']); assert.deepEqual(errors, []);
});

test('stopping a stream preserves partial output and restores composer', async t => {
  const { page, errors } = await fixture(t);
  await page.locator('#cancel').click();
  await page.waitForFunction(() => document.querySelector('#cancel').hidden);
  assert.equal(await page.locator('.body h2').innerText(), 'Linux 内存管理');
  assert.match(await page.locator('.run-label').innerText(), /已停止/);
  assert.equal(await page.locator('.streaming').count(), 0);
  assert.equal(await page.locator('.run-error').count(), 0);
  assert.equal(await page.locator('#send').isVisible(), true);
  assert.deepEqual(errors, []);
});

test('owner SSE authenticates through header and keeps the token out of the URL', async t => {
  const { page, release, auth, errors } = await fixture(t, 'owner');
  assert.deepEqual(auth, ['Bearer valid']);
  release();
  await page.waitForFunction(() => document.querySelector('#cancel').hidden);
  assert.deepEqual(errors, []);
});

test('users can steer or queue a follow-up during streaming and remove pending input', async t => {
  const { page, errors, queueRequests } = await fixture(t);
  assert.equal(await page.locator('#prompt').isDisabled(), false);
  await page.locator('#prompt').fill('重点讲解 VMA');
  await page.locator('#queue-input').click();
  await page.locator('.queued-input').waitFor();
  assert.equal(queueRequests[0].mode, 'steer');
  assert.match(await page.locator('.queued-input').first().innerText(), /补充指令.*等待处理/);
  await page.locator('.queued-input button').first().click();
  await page.waitForFunction(() => document.querySelector('.queued-input').dataset.status === 'removed');
  await page.locator('#input-mode').selectOption('follow_up');
  await page.locator('#prompt').fill('稍后给出示例');
  await page.locator('#queue-input').click();
  await page.waitForFunction(() => document.querySelectorAll('.queued-input').length === 2);
  assert.equal(queueRequests[1].mode, 'follow_up');
  await page.locator('#cancel').click();
  await page.waitForFunction(() => document.querySelector('#cancel').hidden);
  assert.match(await page.locator('.queued-input').last().innerText(), /本次未处理/);
  assert.equal(await page.locator('.queued-input').last().locator('button').isVisible(), false);
  assert.deepEqual(errors, []);
});
