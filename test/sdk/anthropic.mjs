// Anthropic 格式验证（Claude Code / Cline 等客户端）：官方 SDK + 多轮 + 工具
import Anthropic from '@anthropic-ai/sdk';

const BASE = process.env.GW || 'http://localhost:8090';
const client = new Anthropic({ apiKey: 'sk-mimo', baseURL: BASE });

const TOOLS = [
  { name: 'get_weather', description: 'Get current weather for a city.', input_schema: { type: 'object', properties: { city: { type: 'string' } }, required: ['city'] } },
  { name: 'run_command', description: 'Run a shell command.', input_schema: { type: 'object', properties: { command: { type: 'string' } }, required: ['command'] } },
];

let pass = 0, fail = 0;
const record = (ok, name, detail = '') => {
  console.log(`  ${ok ? '[PASS]' : '[FAIL]'} ${name}${detail ? ' — ' + detail : ''}`);
  ok ? pass++ : fail++;
};

async function testBasic(model) {
  console.log(`\n=== [${model}] Anthropic 基础 ===`);
  try {
    const r = await client.messages.create({
      model, max_tokens: 200,
      system: 'You are concise.',
      messages: [{ role: 'user', content: '只回复两个字：收到' }],
    });
    const text = r.content.filter(b => b.type === 'text').map(b => b.text).join('');
    record(text.length > 0, '基础对话', JSON.stringify(text.slice(0, 40)));
    record(!!r.usage, 'usage 字段', `in=${r.usage?.input_tokens} out=${r.usage?.output_tokens}`);
  } catch (e) {
    record(false, '基础对话', e.message.slice(0, 100));
  }
}

async function testMultiTurn(model) {
  console.log(`\n=== [${model}] Anthropic 多轮（增量） ===`);
  const messages = [{ role: 'user', content: '记住数字 5555。只回复"好"' }];
  try {
    const r1 = await client.messages.create({ model, max_tokens: 100, system: 'You are concise.', messages });
    const t1 = r1.content.filter(b => b.type === 'text').map(b => b.text).join('');
    messages.push({ role: 'assistant', content: t1 });
    messages.push({ role: 'user', content: '那个数字是多少？只回答数字' });
    const r2 = await client.messages.create({ model, max_tokens: 100, system: 'You are concise.', messages });
    const t2 = r2.content.filter(b => b.type === 'text').map(b => b.text).join('');
    record(t2.includes('5555'), '第二轮上下文保持', JSON.stringify(t2.slice(0, 40)));
  } catch (e) {
    record(false, '多轮', e.message.slice(0, 100));
  }
}

async function testTools(model) {
  console.log(`\n=== [${model}] Anthropic 工具调用 ===`);
  try {
    const r = await client.messages.create({
      model, max_tokens: 300,
      system: 'You are an agent. Use tools when needed.',
      tools: TOOLS,
      messages: [{ role: 'user', content: '用 get_weather 工具查一下北京的天气' }],
    });
    const toolUses = r.content.filter(b => b.type === 'tool_use');
    const text = r.content.filter(b => b.type === 'text').map(b => b.text).join('');
    record(toolUses.length > 0, 'tool_use 块', toolUses.length ? JSON.stringify(toolUses.map(t => ({ n: t.name, i: t.input }))) : `直接回答: ${text.slice(0, 60)}`);
    if (toolUses.length > 0) {
      const messages = [
        { role: 'user', content: '用 get_weather 工具查一下北京的天气' },
        { role: 'assistant', content: r.content },
        { role: 'user', content: [{ type: 'tool_result', tool_use_id: toolUses[0].id, content: JSON.stringify({ city: '北京', temp: '21°C', condition: '多云' }) }] },
      ];
      const r2 = await client.messages.create({ model, max_tokens: 200, system: 'You are an agent.', tools: TOOLS, messages });
      const t2 = r2.content.filter(b => b.type === 'text').map(b => b.text).join('');
      record(t2.length > 0, '工具结果回传后作答', JSON.stringify(t2.slice(0, 60)));
    }
  } catch (e) {
    record(false, '工具调用', e.message.slice(0, 100));
  }
}

async function testStream(model) {
  console.log(`\n=== [${model}] Anthropic 流式 ===`);
  try {
    const stream = await client.messages.create({
      model, max_tokens: 200, system: 'You are concise.',
      messages: [{ role: 'user', content: '数到三，只输出数字' }],
      stream: true,
    });
    let text = '', events = 0;
    for await (const ev of stream) {
      events++;
      if (ev.type === 'content_block_delta' && ev.delta?.type === 'text_delta') text += ev.delta.text;
    }
    record(text.length > 0, '流式输出', `events=${events} text=${JSON.stringify(text.slice(0, 40))}`);
  } catch (e) {
    record(false, '流式', e.message.slice(0, 100));
  }
}

(async () => {
  console.log(`网关: ${BASE}`);
  for (const m of ['mimo-v2.6-flash', 'mimo-v2.6-pro', 'mimo-v2.6-pro-ultraspeed-studio']) {
    await testBasic(m);
    await testMultiTurn(m);
    await testTools(m);
    await testStream(m);
  }
  console.log(`\n========== Anthropic: ${pass} PASS / ${fail} FAIL ==========`);
})();
