import { marked } from 'marked';
import DOMPurify from 'dompurify';
import hljs from 'highlight.js/lib/common';
import 'highlight.js/styles/github.css';

(() => {
  const $ = id => document.getElementById(id);
  const state = { session: '', run: '', owner: '', epoch: 0, sessions: [], views: new Map(), pollTimer: null, stream: null, follow: true };
  try { state.sessions = JSON.parse(localStorage.getItem('durlim-agent-sessions') || '[]').slice(0, 20); } catch {}
  const welcome = $('welcome').cloneNode(true);

  async function api(path, body, owner = state.owner, method = body === undefined ? 'GET' : 'POST') {
    const headers = { 'Content-Type': 'application/json' };
    if (owner) headers.Authorization = 'Bearer ' + owner;
    const response = await fetch('/api/v1/agent' + path, {
      method, headers,
      body: body === undefined ? undefined : JSON.stringify(body), credentials: 'same-origin',
      signal: AbortSignal.timeout(20000),
    });
    const data = await response.json();
    if (!response.ok) throw new Error(data.error || '请求失败');
    return data;
  }

  function status(text, error = false) { $('run-status').textContent = text; $('run-status').classList.toggle('error', error); }
  function safeURL(raw) { try { const u = new URL(raw); return ['http:', 'https:'].includes(u.protocol) ? u.href : null; } catch { return null; } }
  function followOutput() {
    if (state.follow) $('conversation').scrollTop = $('conversation').scrollHeight;
  }
  function renderText(parent, text, final = true) {
    parent.innerHTML = DOMPurify.sanitize(marked.parse(text || '', { gfm: true, breaks: false }), {
      ALLOWED_TAGS: ['p', 'br', 'strong', 'em', 'del', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'ul', 'ol', 'li', 'blockquote', 'pre', 'code', 'a', 'hr', 'table', 'thead', 'tbody', 'tr', 'th', 'td'],
      ALLOWED_ATTR: ['href', 'title', 'class', 'start', 'align'],
    });
    parent.querySelectorAll('a').forEach(a => {
      const href = safeURL(a.getAttribute('href'));
      if (!href) { a.replaceWith(document.createTextNode(a.textContent)); return; }
      a.href = href; a.target = '_blank'; a.rel = 'noopener noreferrer';
    });
    parent.querySelectorAll('table').forEach(table => {
      const wrap = document.createElement('div'); wrap.className = 'table-scroll';
      table.replaceWith(wrap); wrap.append(table);
    });
    if (!final) return;
    parent.querySelectorAll('pre code').forEach(code => {
      hljs.highlightElement(code);
      const button = document.createElement('button'); button.type = 'button';
      button.className = 'copy-code'; button.textContent = '复制代码';
      button.addEventListener('click', () => copyText(code.textContent, button));
      code.parentElement.append(button);
    });
  }
  async function copyText(text, button) {
    try { await navigator.clipboard.writeText(text); button.textContent = '已复制'; }
    catch { button.textContent = '复制失败，请手动选择'; }
    setTimeout(() => { if (button.isConnected) button.textContent = button.classList.contains('copy-code') ? '复制代码' : '复制回复'; }, 1800);
  }
  function message(role, text, parent = $('conversation')) {
    $('welcome')?.remove();
    const article = document.createElement('article'); article.className = 'message ' + role;
    const speaker = document.createElement('div'); speaker.className = 'speaker'; speaker.textContent = role === 'user' ? '你' : 'Durlim · AI';
    const body = document.createElement('div'); body.className = 'body';
    if (role === 'user') body.textContent = text; else renderText(body, text);
    article.append(speaker, body);
    parent.append(article); followOutput();
    return { article, body, text };
  }
  function renderSources(article, sources = []) {
    article.querySelector('.sources')?.remove();
    const unique = new Map(sources.map(source => [source.url, source]));
    if (unique.size) {
      const list = document.createElement('div'); list.className = 'sources';
      for (const source of unique.values()) { const href = safeURL(source.url); if (!href) continue; const a = document.createElement('a'); a.href = href; a.textContent = source.title; a.target = '_blank'; a.rel = 'noopener noreferrer'; list.append(a); }
      article.append(list);
    }
  }
  function busy(value) {
    $('send').hidden = value; $('cancel').hidden = !value; $('cancel').disabled = false;
    $('new-chat').disabled = value; $('prompt').disabled = value && !state.run;
    $('run-controls').hidden = !value || !state.run;
    $('goal-mode').disabled = value; $('goal').disabled = value;
    $('prompt').placeholder = value && state.run ? '可以补充方向，或排队追问…' : '有什么想聊的？';
    $('chat-form').setAttribute('aria-busy', String(value));
  }
  function listSessions() {
    $('sessions').replaceChildren();
    if (state.owner) return;
    for (const session of state.sessions) {
      const button = document.createElement('button'); button.textContent = session.title;
      if (session.id === state.session) button.setAttribute('aria-current', 'true');
      button.addEventListener('click', () => { if (!state.run) selectSession(session.id); });
      $('sessions').append(button);
    }
  }
  async function selectSession(id) {
    const epoch = ++state.epoch;
    resetTransport(); busy(false);
    try {
      const session = await api('/sessions/' + id);
      if (epoch !== state.epoch) return;
      resetTransport(); state.session = id; state.views.clear(); state.follow = true; $('conversation').replaceChildren();
      if (session.runs?.length) {
        for (const run of session.runs) {
          message('user', run.prompt);
          const view = runView(run.id);
          (run.events || []).forEach(event => applyEvent(view, event));
          finishView(view, run);
        }
      } else session.messages.forEach(m => message(m.role, m.content));
      if (!$('conversation').children.length && !session.active_run) $('conversation').append(welcome.cloneNode(true));
      renderApprovals(session.approvals || []); renderTodos(session.todos || []); listSessions(); status('');
      if (session.active_run) {
        const run = await api('/runs/' + session.active_run);
        if (epoch !== state.epoch) return;
        message('user', run.prompt || '继续上次任务'); state.run = run.id || session.active_run;
        busy(true); watch(state.run, epoch);
      }
    } catch (error) { if (epoch === state.epoch) status(error.message, true); }
  }
  function renderApprovals(items) {
    $('approvals').replaceChildren();
    for (const item of items) {
      const box = document.createElement('div'); box.className = 'approval';
      const title = document.createElement('strong');
      title.textContent = (item.status === 'interrupted' ? '操作结果待核实：' : '待确认操作：') + item.tool;
      const pre = document.createElement('pre'); pre.textContent = JSON.stringify(item.arguments, null, 2);
      box.append(title, pre);
      if (item.status === 'interrupted') {
        const warning = document.createElement('p');
        warning.textContent = '执行时服务发生重启，操作结果待核实。请先检查目标系统的实际结果，再决定是否重新发起；系统不会自动重试。';
        box.append(warning);
        $('approvals').append(box);
        continue;
      }
      for (const [label, allow] of [['允许', true], ['拒绝', false]]) {
        const button = document.createElement('button'); button.textContent = label; button.disabled = !!state.run;
        button.addEventListener('click', async () => {
          box.querySelectorAll('button').forEach(b => b.disabled = true);
          try { const result = await api('/sessions/' + state.session + '/approvals/' + item.id, { allow }); box.remove(); status(result.status === 'failed' ? '操作未完成：' + result.result : allow ? '已确认执行。可继续对话。' : '已拒绝操作。', result.status === 'failed'); }
          catch (error) { status(error.message, true); box.querySelectorAll('button').forEach(b => b.disabled = false); }
        }); box.append(button);
      }
      $('approvals').append(box);
    }
  }
  const toolLabels = { web_search: '搜索网页', web_read: '读取网页', profile_read: '读取个人资料', skills_list: '查看技能', skills_load: '加载技能', todo_write: '更新计划', memory_search: '检索记忆', memory_write: '保存记忆', note_write: '保存笔记', artifact_read: '读取完整结果', subagent: '委派研究', mcp_call: '调用 MCP 工具', workflow_run: '执行工作流' };
  const statusLabels = { completed: '已完成', failed: '执行失败', cancelled: '已停止', interrupted: '服务中断', budget_exhausted: '已达运行上限', blocked: '目标未通过验收', approval_required: '等待确认' };
  const pretty = text => { try { return JSON.stringify(JSON.parse(text), null, 2); } catch { return text || ''; } };
  function runView(id) {
    if (state.views.has(id)) return state.views.get(id);
    $('welcome')?.remove();
    const section = document.createElement('section'); section.className = 'run-block running';
    section.setAttribute('aria-label', 'Agent 执行过程');
    const heading = document.createElement('div'); heading.className = 'run-heading';
    const label = document.createElement('span'); label.className = 'run-label'; label.textContent = '正在思考与查证…';
    const meta = document.createElement('span'); meta.className = 'run-meta';
    heading.append(label, meta);
    const timeline = document.createElement('div'); timeline.className = 'run-timeline';
    section.append(heading, timeline); $('conversation').append(section);
    const view = { id, section, label, meta, timeline, cursor: 0, tools: new Map(), inputs: new Map(), messages: new Map(), steps: new Map(), lastMessage: null, done: false, renderTimer: null };
    state.views.set(id, view); followOutput(); return view;
  }
  function renderTodos(items) {
    $('activity').hidden = !items.length;
    $('todos').replaceChildren(...items.map(todo => {
      const li = document.createElement('li'); li.dataset.status = todo.status;
      li.textContent = ({ pending: '○', in_progress: '◉', completed: '✓' }[todo.status] || '○') + ' ' + todo.content;
      return li;
    }));
  }
  function renderPending(view, final = false) {
    clearTimeout(view.renderTimer); view.renderTimer = null;
    for (const item of view.messages.values()) {
      if (!item.dirty && !final) continue;
      renderText(item.body, item.text, final); item.dirty = false;
    }
    followOutput();
  }
  function applyEvent(view, event) {
    if (event.type === 'GoalCheckStart') {
      view.label.textContent = '正在验收目标…';
    } else if (event.type === 'turn_start' && event.agent === 'lead') {
      view.meta.textContent = '第 ' + event.turn + ' 轮';
    } else if (event.type.startsWith('input_')) {
      renderInput(view, { id: event.input_id, mode: event.mode, content: event.text, status: event.status });
    } else if (event.type === 'model_start') {
      const row = document.createElement('div'); row.className = 'model-step pending';
      row.textContent = event.agent && event.agent !== 'lead' ? event.agent + ' · 正在处理' : (event.turn ? '第 ' + event.turn + ' 轮 · ' : '') + '正在思考…';
      view.timeline.append(row); view.steps.set(event.message_id, row); view.label.textContent = row.textContent;
    } else if (event.type === 'assistant_delta') {
      let item = view.messages.get(event.message_id);
      if (!item) { item = message('assistant', '', view.timeline); item.article.classList.add('streaming'); view.messages.set(event.message_id, item); }
      item.text += event.text; item.dirty = true; view.lastMessage = item;
      view.label.textContent = '正在生成回复…';
      if (!view.renderTimer) view.renderTimer = setTimeout(() => renderPending(view), 60);
    } else if (event.type === 'model_end') {
      renderPending(view, true);
      const item = view.messages.get(event.message_id); item?.article.classList.remove('streaming');
      const step = view.steps.get(event.message_id);
      if (step) { step.classList.remove('pending'); step.textContent = (event.status === 'failed' ? '生成中断' : '已完成一轮处理') + ' · ' + (event.duration_ms / 1000).toFixed(1) + ' 秒'; }
    } else if (event.type === 'tool_start') {
      const details = document.createElement('details'); details.className = 'tool-event pending';
      const summary = document.createElement('summary');
      const title = document.createElement('span'); title.className = 'tool-name'; title.textContent = toolLabels[event.tool] || event.tool;
      const code = document.createElement('code'); code.textContent = event.tool;
      const badge = document.createElement('span'); badge.className = 'tool-status'; badge.textContent = event.status === 'preparing' ? '校验 / 排队中' : '执行中';
      summary.append(title, code, badge); details.append(summary);
      const input = document.createElement('pre'); input.className = 'tool-input'; input.textContent = pretty(event.arguments);
      const inputLabel = document.createElement('div'); inputLabel.className = 'tool-detail-label'; inputLabel.textContent = '调用参数' + (event.agent !== 'lead' && event.agent ? ' · ' + event.agent : '');
      details.append(inputLabel, input); view.timeline.append(details);
      view.tools.set(event.call_id, { details, badge }); view.label.textContent = title.textContent + '…';
    } else if (event.type === 'tool_update') {
      const tool = view.tools.get(event.call_id);
      if (tool) tool.badge.textContent = '执行中';
    } else if (event.type === 'tool_end') {
      const tool = view.tools.get(event.call_id);
      if (tool) {
        tool.details.classList.remove('pending'); tool.details.dataset.status = event.status;
        tool.badge.textContent = (statusLabels[event.status] || event.status) + ' · ' + (event.duration_ms / 1000).toFixed(1) + ' 秒';
        const label = document.createElement('div'); label.className = 'tool-detail-label'; label.textContent = '执行结果';
        const result = document.createElement('pre'); result.className = 'tool-result'; result.textContent = pretty(event.result);
        tool.details.append(label, result);
      }
      if (event.tool === 'todo_write' && event.status === 'completed') { try { renderTodos(JSON.parse(event.result).items || []); } catch {} }
    } else if (['GoalCheck', 'ContextCompact', 'HookWarning'].includes(event.type)) {
      const row = document.createElement('div'); row.className = 'model-step';
      row.textContent = ({ GoalCheck: '目标验收 · ', ContextCompact: '已整理上下文 · ', HookWarning: '执行提示 · ' }[event.type]) + event.text;
      view.timeline.append(row);
    }
    followOutput();
  }
  function finishView(view, run) {
    if (view.done) return;
    view.done = true; renderPending(view, true);
    (run.inputs || []).forEach(item => renderInput(view, item));
    view.section.classList.remove('running'); view.section.dataset.status = run.status;
    view.label.textContent = statusLabels[run.status] || run.status;
    const elapsed = Math.max(0, (Date.parse(run.updated_at) - Date.parse(run.created_at)) / 1000);
    view.meta.textContent = (Number.isFinite(elapsed) ? elapsed.toFixed(1) + ' 秒 · ' : '') + view.tools.size + ' 次工具调用' + (run.calls ? ' · ' + run.calls + ' 轮模型调用' : '');
    for (const step of view.steps.values()) step.classList.remove('pending');
    for (const tool of view.tools.values()) {
      if (tool.details.classList.contains('pending')) { tool.details.classList.remove('pending'); tool.badge.textContent = '已中断，结果未知'; }
    }
    if (run.answer) {
      if (!view.lastMessage) view.lastMessage = message('assistant', run.answer, view.timeline);
      else { view.lastMessage.text = run.answer; renderText(view.lastMessage.body, run.answer); }
    }
    view.section.querySelectorAll('.streaming').forEach(item => item.classList.remove('streaming'));
    if (view.lastMessage) {
      renderSources(view.lastMessage.article, run.sources || []);
      const copy = document.createElement('button'); copy.className = 'copy-answer'; copy.type = 'button'; copy.textContent = '复制回复';
      copy.addEventListener('click', () => copyText(view.lastMessage.text, copy)); view.lastMessage.article.append(copy);
    }
    if (run.error && run.status !== 'cancelled') { const error = document.createElement('p'); error.className = 'run-error'; error.textContent = run.error; view.timeline.append(error); }
    if (!view.tools.size && !view.lastMessage && !run.error) { const hint = document.createElement('p'); hint.className = 'model-step'; hint.textContent = '本次任务未调用工具。'; view.timeline.append(hint); }
    followOutput();
  }
  function renderInput(view, item) {
    let row = view.inputs.get(item.id);
    if (row && item.status === 'pending' && row.status !== 'pending') return;
    if (!row) {
      const box = document.createElement('div'); box.className = 'queued-input';
      const label = document.createElement('span'); label.className = 'input-status';
      const content = document.createElement('div'); content.className = 'input-content';
      const remove = document.createElement('button'); remove.type = 'button'; remove.textContent = '移除';
      remove.addEventListener('click', async () => {
        const epoch = state.epoch, owner = state.owner; remove.disabled = true;
        try {
          await api('/runs/' + view.id + '/inputs/' + encodeURIComponent(item.id), undefined, owner, 'DELETE');
          if (epoch === state.epoch) renderInput(view, { ...item, status: 'removed' });
        } catch (error) { if (epoch === state.epoch) { status(error.message, true); remove.disabled = false; } }
      });
      box.append(label, content, remove); view.timeline.append(box);
      row = { box, label, content, remove }; view.inputs.set(item.id, row);
    }
    row.label.textContent = (item.mode === 'steer' ? '补充指令' : '排队追问') + ' · ' + ({ pending: '等待处理', delivered: '已加入上下文', removed: '已移除', unprocessed: '本次未处理' }[item.status] || item.status);
    row.content.textContent = item.content; row.box.dataset.status = item.status;
    row.status = item.status;
    row.remove.hidden = item.status !== 'pending' || view.done; row.remove.disabled = false;
  }
  let pendingInput = null, queueSubmitting = false;
  async function queueMessage(text) {
    if (queueSubmitting) return;
    const id = state.run, epoch = state.epoch, owner = state.owner, mode = $('input-mode').value;
    if (!pendingInput || pendingInput.run !== id || pendingInput.message !== text || pendingInput.mode !== mode) {
      pendingInput = { run: id, id: crypto.randomUUID(), message: text, mode };
    }
    const request = pendingInput; queueSubmitting = true; $('queue-input').disabled = true;
    try {
      const item = await api('/runs/' + id + '/inputs', { id: request.id, message: text, mode }, owner);
      if (epoch !== state.epoch) return;
      if (state.run === id) renderInput(runView(id), item);
      if ($('prompt').value.trim() === text) { $('prompt').value = ''; resizePrompt(); }
      pendingInput = null;
      if (state.run === id) status(item.status === 'delivered' ? '补充指令已加入上下文。' : '已排队；可在执行时间线中移除。');
      else status('补充消息已记录，处理状态见执行时间线。');
    } catch (error) { if (epoch === state.epoch) status(error.message, true); }
    finally { if (epoch === state.epoch) { queueSubmitting = false; $('queue-input').disabled = false; } }
  }
  function resetTransport() {
    pendingInput = null; queueSubmitting = false; $('queue-input').disabled = false;
    clearTimeout(state.pollTimer); state.pollTimer = null;
    state.stream?.abort(); state.stream = null;
    for (const view of state.views.values()) clearTimeout(view.renderTimer);
  }
  async function finishRun(view, run, epoch) {
    if (epoch !== state.epoch || state.run !== run.id) return;
    finishView(view, run); state.run = ''; busy(false);
    status(run.status === 'completed' ? '' : statusLabels[run.status] || run.error, run.status !== 'cancelled' && run.status !== 'completed');
    $('prompt').focus();
    try {
      const session = await api('/sessions/' + state.session);
      if (epoch !== state.epoch) return;
      renderApprovals(session.approvals || []); renderTodos(session.todos || []);
    } catch (error) { if (epoch === state.epoch) status(error.message, true); }
  }
  async function watch(id, epoch, failures = 0) {
    if (epoch !== state.epoch || state.run !== id) return;
    const view = runView(id), controller = new AbortController(); state.stream = controller;
    let idleTimer;
    const touch = () => { clearTimeout(idleTimer); idleTimer = setTimeout(() => controller.abort(), 45000); };
    touch(); status(failures ? '正在重连，已保留当前输出…' : '正在思考与查证…');
    try {
      const headers = { Accept: 'text/event-stream' }; if (state.owner) headers.Authorization = 'Bearer ' + state.owner;
      const response = await fetch('/api/v1/agent/runs/' + id + '/events?after=' + view.cursor, { headers, credentials: 'same-origin', signal: controller.signal });
      if (!response.ok || !response.headers.get('content-type')?.includes('text/event-stream')) throw new Error('实时连接暂不可用');
      const reader = response.body.getReader(), decoder = new TextDecoder();
      let buffer = '';
      try {
        while (true) {
          const { value, done } = await reader.read();
          if (epoch !== state.epoch || state.run !== id) return;
          if (done) throw new Error('实时连接已断开');
          touch(); buffer += decoder.decode(value, { stream: true });
          buffer = buffer.replace(/\r\n/g, '\n');
          let boundary;
          while ((boundary = buffer.indexOf('\n\n')) >= 0) {
            const frame = buffer.slice(0, boundary); buffer = buffer.slice(boundary + 2);
            let kind = '', sequence = 0; const data = [];
            for (const line of frame.split('\n')) {
              if (line.startsWith('event:')) kind = line.slice(6).trim();
              if (line.startsWith('id:')) sequence = Number(line.slice(3).trim());
              if (line.startsWith('data:')) data.push(line.slice(5).trimStart());
            }
            if (!data.length) continue;
            const payload = JSON.parse(data.join('\n'));
            if (kind === 'activity' && sequence > view.cursor) { applyEvent(view, payload); view.cursor = sequence; failures = 0; }
            if (kind === 'done') { await finishRun(view, payload, epoch); return; }
          }
        }
      } finally { await reader.cancel().catch(() => {}); }
    } catch (error) {
      if (epoch !== state.epoch || state.run !== id) return;
      try {
        const run = await api('/runs/' + id);
        if (epoch !== state.epoch || state.run !== id) return;
        for (const event of (run.events || []).slice(view.cursor)) { applyEvent(view, event); view.cursor++; }
        if (run.status !== 'running') { await finishRun(view, { ...run, id }, epoch); return; }
        status('实时连接中断，正在重连；当前输出已保留。');
      } catch (fallbackError) { status(fallbackError.message + '，正在重连…', true); }
      state.pollTimer = setTimeout(() => watch(id, epoch, failures + 1), Math.min(1000 * 2 ** failures, 8000));
    } finally { clearTimeout(idleTimer); if (state.stream === controller) state.stream = null; }
  }
  $('chat-form').addEventListener('submit', async event => {
    event.preventDefault();
    const text = $('prompt').value.trim(); if (!text) return;
    if (state.run) { await queueMessage(text); return; }
    const epoch = state.epoch, owner = state.owner;
    busy(true);
    try {
      if (!state.session) {
        const session = await api('/sessions', {}, owner);
        if (epoch !== state.epoch) return;
        state.session = session.id;
        if (!state.owner) { state.sessions.unshift({ id: session.id, title: text.slice(0, 24) }); state.sessions = state.sessions.slice(0, 20); localStorage.setItem('durlim-agent-sessions', JSON.stringify(state.sessions)); }
        listSessions();
      }
      const goal = $('goal-mode').checked ? $('goal').value.trim() : '';
      if ($('goal-mode').checked && !goal) throw new Error('请填写目标完成标准');
      const result = await api('/sessions/' + state.session + '/messages', { message: text, goal }, owner);
      if (epoch !== state.epoch) {
        api('/runs/' + result.run_id + '/cancel', {}, owner).catch(() => {});
        return;
      }
      state.follow = true; message('user', text); $('prompt').value = ''; resizePrompt(); state.run = result.run_id; busy(true); watch(state.run, state.epoch);
    } catch (error) { if (epoch !== state.epoch) return; busy(false); status(error.message, true); }
  });
  $('cancel').addEventListener('click', async () => { if (!state.run) return; $('cancel').disabled = true; try { await api('/runs/' + state.run + '/cancel', {}); status('正在停止…'); } catch (e) { status(e.message, true); $('cancel').disabled = false; } });
  $('goal-mode').addEventListener('change', () => { $('goal').hidden = !$('goal-mode').checked; });
  $('new-chat').addEventListener('click', () => { if (state.run) return; state.epoch++; resetTransport(); state.session = ''; state.views.clear(); state.follow = true; $('conversation').replaceChildren(welcome.cloneNode(true)); $('approvals').replaceChildren(); renderTodos([]); status(''); listSessions(); $('prompt').focus(); });
  $('conversation').addEventListener('click', event => { const button = event.target.closest('[data-prompt]'); if (button) { $('prompt').value = button.dataset.prompt; $('prompt').focus(); } });
  $('prompt').addEventListener('keydown', event => { if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) { event.preventDefault(); $('chat-form').requestSubmit(); } });
  function resizePrompt() { $('prompt').style.height = 'auto'; $('prompt').style.height = Math.min($('prompt').scrollHeight, 180) + 'px'; }
  $('prompt').addEventListener('input', resizePrompt);
  $('conversation').addEventListener('scroll', () => {
    const el = $('conversation'); state.follow = el.scrollHeight - el.scrollTop - el.clientHeight < 100;
    $('jump-bottom').hidden = state.follow;
  });
  $('jump-bottom').addEventListener('click', () => { state.follow = true; followOutput(); $('jump-bottom').hidden = true; });
  window.addEventListener('pagehide', resetTransport);
  $('owner-toggle').addEventListener('click', () => $('owner-dialog').showModal());
  $('owner-login').addEventListener('submit', async event => {
    event.preventDefault();
    const token = $('owner-token').value; $('owner-token').value = '';
    const button = $('owner-login').querySelector('button'); button.disabled = true;
    try {
      const info = await api('/info', undefined, token);
      if (!info.owner) throw new Error('令牌无效');
      const oldRun = state.run, oldOwner = state.owner;
      state.epoch++; resetTransport();
      state.owner = token; state.session = ''; state.run = ''; state.views.clear();
      busy(false); status('');
      $('prompt').value = '';
      $('conversation').replaceChildren(welcome.cloneNode(true));
      $('approvals').replaceChildren(); $('activity').hidden = true;
      $('sessions').replaceChildren();
      $('owner-tools').hidden = false; $('owner-login').hidden = true;
      $('owner-status').textContent = '已连接。令牌仅保存在当前页面内存。';
      if (oldRun) {
        try { await api('/runs/' + oldRun + '/cancel', {}, oldOwner); }
        catch { $('owner-status').textContent += ' 旧任务停止请求失败，将受服务端运行时限约束。'; }
      }
      await ownerRefresh();
    } catch (e) { $('owner-status').textContent = e.message; }
    finally { button.disabled = false; }
  });
  $('owner-logout').addEventListener('click', () => location.reload());
  async function ownerRefresh() {
    try { const data = await api('/owner/state'); $('owner-jobs').replaceChildren(); for (const job of data.jobs) { const row = document.createElement('div'); row.className = 'job'; row.textContent = job.prompt + '\n' + job.status + (job.cron ? ' · ' + job.cron : '') + (job.last_error ? '\n' + job.last_error : ''); const button = document.createElement('button'); button.textContent = '取消'; button.disabled = job.status !== 'active'; button.addEventListener('click', async () => { try { await ownerTool('job_cancel', { id: job.id }); ownerRefresh(); } catch(e) { $('owner-status').textContent = e.message; } }); row.append(button); $('owner-jobs').append(row); } }
    catch (e) { $('owner-status').textContent = e.message; }
  }
  async function ownerTool(name, args) { if (!state.session) state.session = (await api('/sessions', {})).id; return api('/owner/tools/' + name, { session_id: state.session, arguments: args }); }
  $('owner-refresh').addEventListener('click', ownerRefresh);
  $('job-form').addEventListener('submit', async event => {
    event.preventDefault();
    try { const cron = $('job-cron').value.trim(); const result = await ownerTool(cron ? 'schedule_cron' : 'background_start', { prompt: $('job-prompt').value, cron }); if (result.error) throw new Error(result.error); $('owner-status').textContent = result.status === 'approval_required' ? '定时任务等待对话页中的确认。' : '后台任务已创建。'; if (result.status === 'approval_required') { const session = await api('/sessions/' + state.session); renderApprovals(session.approvals); } await ownerRefresh(); }
    catch (e) { $('owner-status').textContent = e.message; }
  });
  listSessions();
  api('/info').then(info => { $('connection').textContent = info.configured ? 'DeepSeek · 在线' : '服务未配置'; if (!info.configured) $('send').disabled = true; }).catch(() => { $('connection').textContent = '连接不可用'; });
})();
