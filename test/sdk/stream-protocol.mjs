// 重点验证：流式工具调用的协议正确性（finish_reason / index / usage 块）
import OpenAI from 'openai';

const BASE = process.env.GW || 'http://localhost:8090';
const client = new OpenAI({ apiKey: 'sk-mimo', baseURL: BASE });

const TOOLS = [
  { type: 'function', function: { name: 'read', description: 'Read a file.', parameters: { type: 'object', properties: { file_path: { type: 'string', description: 'Path' } }, required: ['file_path'] } } },
  { type: 'function', function: { name: 'pwsh', description: 'Run a command.', parameters: { type: 'object', properties: { command: { type: 'string' }, description: { type: 'string' } }, required: ['command', 'description'] } } },
];

let pass = 0, fail = 0;
const rec = (ok, n, d = '') => { console.log(`  ${ok ? '[PASS]' : '[FAIL]'} ${n}${d ? ' — ' + d : ''}`); ok ? pass++ : fail++; };

// 原始 SSE 抓取：验证 chunk 结构与顺序
async function rawStream(model, messages) {
  const r = await fetch(BASE + '/v1/chat/completions', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: 'Bearer sk-mimo' },
    body: JSON.stringify({ model, messages, tools: TOOLS, stream: true, stream_options: { include_usage: true } }),
  });
  const text = await r.text();
  const chunks = [];
  for (const line of text.split('\n')) {
    if (!line.startsWith('data:')) continue;
    const p = line.slice(5).trim();
    if (p === '[DONE]') { chunks.push({ done: true }); continue; }
    try { chunks.push(JSON.parse(p)); } catch {}
  }
  return { status: r.status, chunks };
}

(async () => {
  for (const model of ['mimo-v2.6-pro', 'mimo-v2.6-pro-ultraspeed-studio', 'mimo-v2.6-flash']) {
    console.log(`\n===== ${model} =====`);
    const { chunks } = await rawStream(model, [{ role: 'user', content: '用 read 工具读一下 C:\\work\\a.txt' }]);

    const data = chunks.filter(c => !c.done);
    const ids = new Set(data.map(c => c.id));
    const withChoices = data.filter(c => Array.isArray(c.choices) && c.choices.length > 0);
    const emptyChoices = data.filter(c => Array.isArray(c.choices) && c.choices.length === 0);
    const toolChunk = withChoices.find(c => c.choices[0].delta?.tool_calls);
    const finishReasons = withChoices.map(c => c.choices[0].finish_reason).filter(Boolean);
    const usageChunk = data.find(c => c.usage);

    console.log(`  chunk 数=${data.length} 有choices=${withChoices.length} 空choices=${emptyChoices.length}`);
    console.log(`  id 唯一数=${ids.size}（应为 1）`);
    console.log(`  finish_reason 序列=${JSON.stringify(finishReasons)}`);
    console.log(`  usage 块 choices=${JSON.stringify(usageChunk?.choices)}`);

    rec(ids.size === 1, '同一流共用一个 id', `${ids.size} 个`);
    rec(!finishReasons.includes('stop') || !toolChunk, '工具调用后 finish_reason 未被 stop 覆盖', JSON.stringify(finishReasons));
    rec(!!usageChunk && Array.isArray(usageChunk.choices) && usageChunk.choices.length === 0, 'usage 块 choices 为空数组');
    if (toolChunk) {
      const tc = toolChunk.choices[0].delta.tool_calls[0];
      rec(tc.index === 0, 'tool_calls 带 index', `index=${tc.index}`);
      rec(!!tc.id && !!tc.function?.name, 'tool_calls 含 id 与 name', `${tc.id} ${tc.function?.name}`);
      rec(typeof tc.function?.arguments === 'string', 'arguments 为 JSON 字符串');
    } else {
      rec(false, '应产生工具调用', '本轮模型未调用工具');
    }
    rec(chunks[chunks.length - 1]?.done === true, '以 [DONE] 结束');

    // 用官方 SDK 再走一遍，确认聚合结果正确
    const stream = await client.chat.completions.create({
      model, messages: [{ role: 'user', content: '用 read 工具读一下 C:\\work\\a.txt' }], tools: TOOLS, stream: true,
    });
    let sdkTools = [], sdkFinish = null, sdkUsage = null;
    for await (const c of stream) {
      if (c.usage) sdkUsage = c.usage;
      const ch = c.choices?.[0];
      if (ch?.finish_reason) sdkFinish = ch.finish_reason;
      if (ch?.delta?.tool_calls) {
        for (const t of ch.delta.tool_calls) {
          const i = t.index ?? 0;
          sdkTools[i] = sdkTools[i] || { id: t.id, name: '', args: '' };
          if (t.id) sdkTools[i].id = t.id;
          if (t.function?.name) sdkTools[i].name += t.function.name;
          if (t.function?.arguments) sdkTools[i].args += t.function.arguments;
        }
      }
    }
    const calls = sdkTools.filter(Boolean);
    rec(calls.length > 0, 'SDK 聚合出工具调用', JSON.stringify(calls.map(c => ({ n: c.name, a: c.args }))));
    rec(sdkFinish === 'tool_calls', 'SDK 最终 finish_reason=tool_calls', String(sdkFinish));
    rec(!!sdkUsage, 'SDK 收到 usage', JSON.stringify(sdkUsage));
  }
  console.log(`\n========== 流式协议: ${pass} PASS / ${fail} FAIL ==========`);
})();
