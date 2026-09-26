// 验证上一轮修复但尚未端到端测试的项目
import { solidPng } from './make-png.mjs';
const BASE = process.env.GW || 'http://localhost:8090';
const KEY = 'sk-mimo';

let pass = 0, fail = 0;
const rec = (ok, n, d = '') => { console.log(`  ${ok ? '[PASS]' : '[FAIL]'} ${n}${d ? ' — ' + d : ''}`); ok ? pass++ : fail++; };

async function post(path, body, headers = {}) {
  const r = await fetch(BASE + path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${KEY}`, ...headers },
    body: JSON.stringify(body),
  });
  const text = await r.text();
  let json = null;
  try { json = JSON.parse(text); } catch {}
  return { status: r.status, json, text, headers: r.headers };
}
async function admin(path, opts = {}) {
  const r = await fetch(BASE + path, {
    ...opts,
    headers: { Authorization: `Bearer ${KEY}`, 'Content-Type': 'application/json', ...(opts.headers || {}) },
  });
  const text = await r.text();
  let json = null; try { json = JSON.parse(text); } catch {}
  return { status: r.status, json, text };
}

(async () => {
  console.log('########## A. default_model 配置是否生效 ##########');
  // 测试配置里 default_model = mimo-v2.6-flash；不指定 model 时应路由到 flash
  let r = await post('/v1/chat/completions', { messages: [{ role: 'user', content: '只回复 OK' }] });
  const modelUsed = r.json?.model;
  rec(modelUsed === 'mimo-v2.6-flash', '未指定模型时使用配置的 default_model', `实际=${modelUsed}`);

  // 管理接口改配置 → 是否即时生效
  const cfgNow = await admin('/admin/api/config');
  rec(cfgNow.status === 200, '读取配置', JSON.stringify(cfgNow.json).slice(0, 80));
  const upd = await admin('/admin/api/config', { method: 'POST', body: JSON.stringify({ default_model: 'mimo-v2.6-pro-ultraspeed-studio' }) });
  rec(upd.status === 200, '更新 default_model', JSON.stringify(upd.json).slice(0, 60));
  await new Promise(x => setTimeout(x, 500));
  r = await post('/v1/chat/completions', { messages: [{ role: 'user', content: '只回复 OK' }] });
  rec(r.json?.model === 'mimo-v2.6-pro-ultraspeed-studio', '改配置后即时生效', `实际=${r.json?.model}`);
  // 显式指定模型仍然优先
  r = await post('/v1/chat/completions', { model: 'mimo-v2.6-pro', messages: [{ role: 'user', content: '只回复 OK' }] });
  rec(r.json?.model === 'mimo-v2.6-pro', '显式指定模型优先于默认值', `实际=${r.json?.model}`);
  // 还原
  await admin('/admin/api/config', { method: 'POST', body: JSON.stringify({ default_model: 'mimo-v2.6-flash' }) });

  console.log('\n########## B. Anthropic system 为 content block 数组 ##########');
  // Claude Code 的形态：system 是数组，带 cache_control
  r = await post('/v1/messages', {
    model: 'mimo-v2.6-flash', max_tokens: 100,
    system: [
      { type: 'text', text: 'You are concise.', cache_control: { type: 'ephemeral' } },
      { type: 'text', text: 'Always answer in Chinese.' },
    ],
    messages: [{ role: 'user', content: '只回复两个字：收到' }],
  }, { 'x-api-key': KEY, 'anthropic-version': '2023-06-01' });
  const sysOk = r.status === 200 && Array.isArray(r.json?.content);
  const sysText = r.json?.content?.filter(b => b.type === 'text').map(b => b.text).join('') || '';
  rec(sysOk, 'system 数组被接受', `status=${r.status}`);
  rec(sysText.includes('收到'), 'system 数组内容生效', JSON.stringify(sysText.slice(0, 30)));

  console.log('\n########## C. Anthropic 多轮工具调用（历史含 tool_use） ##########');
  const TOOLS = [{ name: 'read_file', description: 'Read a file.', input_schema: { type: 'object', properties: { path: { type: 'string' } }, required: ['path'] } }];
  let t = await post('/v1/messages', {
    model: 'mimo-v2.6-flash', max_tokens: 300, tools: TOOLS,
    messages: [{ role: 'user', content: '用 read_file 读 /etc/hostname，然后告诉我内容。' }],
  }, { 'x-api-key': KEY, 'anthropic-version': '2023-06-01' });
  const toolUses = (t.json?.content || []).filter(b => b.type === 'tool_use');
  rec(toolUses.length > 0, '第一轮产生 tool_use', JSON.stringify(toolUses.map(x => ({ n: x.name, i: x.input }))));

  if (toolUses.length > 0) {
    // 第二轮：把 assistant 的 tool_use 与 tool_result 都带上（多轮工具循环）
    const r2 = await post('/v1/messages', {
      model: 'mimo-v2.6-flash', max_tokens: 300, tools: TOOLS,
      messages: [
        { role: 'user', content: '用 read_file 读 /etc/hostname，然后告诉我内容。' },
        { role: 'assistant', content: t.json.content },
        { role: 'user', content: [{ type: 'tool_result', tool_use_id: toolUses[0].id, content: 'build-runner-07' }] },
      ],
    }, { 'x-api-key': KEY, 'anthropic-version': '2023-06-01' });
    const txt2 = (r2.json?.content || []).filter(b => b.type === 'text').map(b => b.text).join('');
    rec(txt2.includes('build-runner-07'), '工具结果回传后正确作答', JSON.stringify(txt2.slice(0, 60)));

    // 第三轮：再追加一轮，验证历史里的 tool_use 被正确重放（否则模型会忘记调过什么）
    const r3 = await post('/v1/messages', {
      model: 'mimo-v2.6-flash', max_tokens: 200, tools: TOOLS,
      messages: [
        { role: 'user', content: '用 read_file 读 /etc/hostname，然后告诉我内容。' },
        { role: 'assistant', content: t.json.content },
        { role: 'user', content: [{ type: 'tool_result', tool_use_id: toolUses[0].id, content: 'build-runner-07' }] },
        { role: 'assistant', content: [{ type: 'text', text: txt2 }] },
        { role: 'user', content: '你刚才用什么工具读的？只回答工具名。' },
      ],
    }, { 'x-api-key': KEY, 'anthropic-version': '2023-06-01' });
    const txt3 = (r3.json?.content || []).filter(b => b.type === 'text').map(b => b.text).join('');
    rec(/read_file/.test(txt3), '第三轮仍记得用过 read_file', JSON.stringify(txt3.slice(0, 50)));
  }

  console.log('\n########## D. 多模态（图片）请求 ##########');
  // 用正常尺寸的纯色图：1x1 透明图上游会回拒答语，测不出网关行为
  const png = solidPng(128, 128, [220, 30, 30]).toString('base64');
  r = await post('/v1/chat/completions', {
    messages: [{
      role: 'user',
      content: [
        { type: 'text', text: '这张图是什么颜色？只回答颜色。' },
        { type: 'image_url', image_url: { url: `data:image/png;base64,${png}` } },
      ],
    }],
  });
  rec(r.status === 200, '图片请求被接受', `status=${r.status} model=${r.json?.model}`);
  const imgReply = r.json?.choices?.[0]?.message?.content || '';
  rec(/红/.test(imgReply), '模型识别出图片颜色', JSON.stringify(imgReply.slice(0, 50)));

  console.log('\n########## E. 管理接口 ##########');
  const accounts = await admin('/admin/api/config');
  rec(accounts.status === 200 && Array.isArray(accounts.json?.accounts), '读取配置（含账号列表）', '账号数=' + (accounts.json?.accounts?.length ?? 0));
  const stats = await admin('/admin/api/stats');
  rec(stats.status === 200, '读取统计', JSON.stringify(stats.json).slice(0, 80));
  const health = await admin('/admin/api/health');
  rec(health.status === 200 && Object.keys(health.json || {}).length > 0, '健康检查返回账号状态', JSON.stringify(health.json).slice(0, 60));

  console.log(`\n========== 未验证修复: ${pass} PASS / ${fail} FAIL ==========`);
})();
