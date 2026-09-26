// 验证：Anthropic 流式工具调用是否符合规范、官方 SDK 能否正确解析
import Anthropic from '@anthropic-ai/sdk';

const BASE = process.env.GW || 'http://localhost:8090';
const client = new Anthropic({ apiKey: 'sk-mimo', baseURL: BASE });

const TOOLS = [
  { name: 'read_file', description: 'Read a file.', input_schema: { type: 'object', properties: { path: { type: 'string' } }, required: ['path'] } },
];

(async () => {
  console.log('=== 1. 原始 SSE 事件序列 ===');
  const r = await fetch(BASE + '/v1/messages', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'x-api-key': 'sk-mimo', 'anthropic-version': '2023-06-01' },
    body: JSON.stringify({
      model: 'mimo-v2.6-pro', max_tokens: 300, stream: true, tools: TOOLS,
      messages: [{ role: 'user', content: '用 read_file 工具读一下 /etc/hostname' }],
    }),
  });
  const raw = await r.text();
  const events = [];
  for (const block of raw.split('\n\n')) {
    const ev = block.match(/^event: (\S+)/m);
    const dt = block.match(/^data: (.+)$/m);
    if (ev) events.push({ event: ev[1], data: dt ? dt[1] : '' });
  }
  console.log('事件序列:', events.map(e => e.event).join(' → '));
  const toolStart = events.find(e => e.event === 'content_block_start' && e.data.includes('tool_use'));
  console.log('content_block_start(tool_use):', toolStart ? toolStart.data.slice(0, 160) : '无');
  const deltas = events.filter(e => e.event === 'content_block_delta');
  console.log('content_block_delta 数量:', deltas.length, deltas.length ? deltas[0].data.slice(0, 120) : '（缺失！）');

  console.log('\n=== 2. 官方 SDK 流式解析（工具调用）===');
  try {
    const stream = await client.messages.create({
      model: 'mimo-v2.6-pro', max_tokens: 300, stream: true, tools: TOOLS,
      messages: [{ role: 'user', content: '用 read_file 工具读一下 /etc/hostname' }],
    });
    const blocks = [];
    let text = '';
    for await (const ev of stream) {
      if (ev.type === 'content_block_start') blocks[ev.index] = { type: ev.content_block.type, name: ev.content_block.name, input: ev.content_block.input, json: '' };
      if (ev.type === 'content_block_delta') {
        if (ev.delta.type === 'text_delta') text += ev.delta.text;
        if (ev.delta.type === 'input_json_delta') blocks[ev.index].json += ev.delta.partial_json;
      }
    }
    console.log('  ✅ SDK 解析成功（未抛异常）');
    console.log('  文本:', JSON.stringify(text.slice(0, 40)));
    blocks.filter(Boolean).forEach((b, i) => console.log(`  block[${i}]: type=${b.type} name=${b.name || '-'} input=${JSON.stringify(b.input)} jsonDelta=${JSON.stringify(b.json || '')}`));
  } catch (e) {
    console.log('  ❌ SDK 抛异常:', e.constructor.name, e.message.slice(0, 200));
  }

  console.log('\n=== 3. SDK 聚合后的最终消息（stream.finalMessage 等价的非流式对照）===');
  try {
    const nonStream = await client.messages.create({
      model: 'mimo-v2.6-pro', max_tokens: 300, tools: TOOLS,
      messages: [{ role: 'user', content: '用 read_file 工具读一下 /etc/hostname' }],
    });
    console.log('  非流式 tool_use:', JSON.stringify(nonStream.content.filter(b => b.type === 'tool_use').map(b => ({ n: b.name, i: b.input }))));
  } catch (e) {
    console.log('  ❌ 非流式失败:', e.message.slice(0, 150));
  }
})();
