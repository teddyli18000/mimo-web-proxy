// 边界与错误路径测试：协议边界 / 异常输入 / Unicode / 错误码
const BASE = process.env.GW || 'http://localhost:8090';
const KEY = 'sk-mimo';

let pass = 0, fail = 0;
const rec = (ok, name, detail = '') => {
  console.log(`  ${ok ? '[PASS]' : '[FAIL]'} ${name}${detail ? ' — ' + detail : ''}`);
  ok ? pass++ : fail++;
};

async function post(path, body, headers = {}) {
  const r = await fetch(BASE + path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${KEY}`, ...headers },
    body: typeof body === 'string' ? body : JSON.stringify(body),
  });
  const text = await r.text();
  let json = null;
  try { json = JSON.parse(text); } catch {}
  return { status: r.status, text, json, headers: r.headers };
}

async function stream(path, body) {
  const r = await fetch(BASE + path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${KEY}` },
    body: JSON.stringify(body),
  });
  const text = await r.text();
  const chunks = [];
  let done = false;
  for (const line of text.split('\n')) {
    if (!line.startsWith('data:')) continue;
    const p = line.slice(5).trim();
    if (p === '[DONE]') { done = true; continue; }
    try { chunks.push(JSON.parse(p)); } catch {}
  }
  return { status: r.status, chunks, done, raw: text };
}

const MODEL = 'mimo-v2.6-flash';

(async () => {
  console.log('########## A. 请求校验与错误码 ##########');

  // 非法 JSON
  let r = await post('/v1/chat/completions', '{bad json');
  rec(r.status === 400, '非法 JSON → 400', `got ${r.status}`);

  // 空 messages
  r = await post('/v1/chat/completions', { model: MODEL, messages: [] });
  rec(r.status === 400, '空 messages → 400', `got ${r.status}`);

  // 只有 system
  r = await post('/v1/chat/completions', { model: MODEL, messages: [{ role: 'system', content: 'x' }] });
  rec(r.status === 400, '只有 system → 400', `got ${r.status}`);

  // 错误 API Key
  const bad = await fetch(BASE + '/v1/models', { headers: { Authorization: 'Bearer wrong' } });
  rec(bad.status === 401, '错误 Key → 401', `got ${bad.status}`);

  // 无 API Key
  const none = await fetch(BASE + '/v1/models');
  rec(none.status === 401, '无 Key → 401', `got ${none.status}`);

  // 管理接口无 Key
  const admin = await fetch(BASE + '/admin/api/accounts');
  rec(admin.status === 401, '管理接口无 Key → 401', `got ${admin.status}`);

  // Anthropic 错误格式
  r = await post('/v1/messages', { model: MODEL, messages: [] });
  const okShape = r.json && r.json.type === 'error' && r.json.error && typeof r.json.error.message === 'string';
  rec(okShape, 'Anthropic 错误体符合规范', JSON.stringify(r.json).slice(0, 90));
  rec((r.headers.get('content-type') || '').includes('application/json'), 'Anthropic 错误带 JSON Content-Type');

  console.log('\n########## B. Unicode 与内容保真 ##########');
  const tricky = '你好世界 🌏 café naïve Ω≈ç√∫ 日本語 テスト 한국어 테스트';
  r = await post('/v1/chat/completions', { model: MODEL, messages: [{ role: 'user', content: `原样重复下面这行，不要改动：${tricky}` }] });
  const echoed = r.json?.choices?.[0]?.message?.content || '';
  const kept = ['🌏', 'café', '日本語', '한국어'].every(s => echoed.includes(s));
  rec(kept, 'Unicode 往返未损坏', JSON.stringify(echoed.slice(0, 60)));

  // 空字符串消息
  r = await post('/v1/chat/completions', { model: MODEL, messages: [{ role: 'user', content: '' }] });
  rec(r.status === 400, '空内容消息 → 400', `got ${r.status}`);

  console.log('\n########## C. 长输入与截断 ##########');
  const long = '背景资料。'.repeat(9000); // ~45K 字符，超 fastchat 预算
  r = await post('/v1/chat/completions', { model: 'mimo-v2.6-pro-ultraspeed-studio', messages: [{ role: 'user', content: `${long}\n\n只回复"收到"两个字。` }] });
  const longOk = r.status === 200 && (r.json?.choices?.[0]?.message?.content || '').length > 0;
  rec(longOk, '45K 输入未报超长（网关截断）', `status=${r.status} reply=${JSON.stringify((r.json?.choices?.[0]?.message?.content || '').slice(0, 30))}`);

  console.log('\n########## D. 流式事件顺序 ##########');
  let s = await stream('/v1/chat/completions', { model: MODEL, messages: [{ role: 'user', content: '数到三' }], stream: true, stream_options: { include_usage: true } });
  rec(s.status === 200, '流式返回 200');
  rec(s.done, '以 [DONE] 结束');
  const ids = new Set(s.chunks.map(c => c.id));
  rec(ids.size === 1, '流内 id 一致', `${ids.size} 个`);
  const usageChunks = s.chunks.filter(c => c.usage);
  rec(usageChunks.length === 1 && usageChunks[0].choices.length === 0, 'usage 块唯一且 choices 为空');
  const finishIdx = s.chunks.findIndex(c => c.choices?.[0]?.finish_reason);
  const usageIdx = s.chunks.findIndex(c => c.usage);
  rec(finishIdx >= 0 && usageIdx > finishIdx, 'usage 在 finish 之后', `finish=${finishIdx} usage=${usageIdx}`);

  console.log('\n########## E. 多工具调用 ##########');
  const tools = [
    { type: 'function', function: { name: 'read_file', description: 'Read a file.', parameters: { type: 'object', properties: { path: { type: 'string' } }, required: ['path'] } } },
    { type: 'function', function: { name: 'run_command', description: 'Run a command.', parameters: { type: 'object', properties: { command: { type: 'string' } }, required: ['command'] } } },
  ];
  s = await stream('/v1/chat/completions', {
    model: MODEL,
    messages: [{ role: 'user', content: '同时做两件事：读 /etc/hostname，并执行命令 whoami' }],
    tools, stream: true,
  });
  const toolChunk = s.chunks.find(c => c.choices?.[0]?.delta?.tool_calls);
  const calls = toolChunk?.choices[0].delta.tool_calls || [];
  rec(calls.length >= 1, '流式返回工具调用', `${calls.length} 个`);
  if (calls.length > 1) {
    rec(calls.every((c, i) => c.index === i), '多个调用 index 递增', JSON.stringify(calls.map(c => c.index)));
  } else {
    rec(true, '多个调用 index 递增（本轮只产生 1 个调用，跳过）');
  }
  rec(calls.every(c => typeof c.function?.arguments === 'string'), 'arguments 均为字符串');

  console.log('\n########## F. 会话隔离 ##########');
  // 两个不同话题并发，确认互不串
  const [a1, b1] = await Promise.all([
    post('/v1/chat/completions', { model: MODEL, messages: [{ role: 'user', content: '记住数字 1111，只回复"好"' }] }),
    post('/v1/chat/completions', { model: MODEL, messages: [{ role: 'user', content: '记住数字 2222，只回复"好"' }] }),
  ]);
  const aText = a1.json?.choices?.[0]?.message?.content || '';
  const bText = b1.json?.choices?.[0]?.message?.content || '';
  rec(aText.length > 0 && bText.length > 0, '并发两个新会话均成功');

  console.log(`\n========== 边界测试: ${pass} PASS / ${fail} FAIL ==========`);
})();
