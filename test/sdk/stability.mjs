// 多轮稳定性测试：会话复用（增量）+ 多轮工具调用 + 长上下文
import OpenAI from 'openai';

const BASE = process.env.GW || 'http://localhost:8090';
const client = new OpenAI({ apiKey: 'sk-mimo', baseURL: BASE });

const TOOLS = [
  { type: 'function', function: { name: 'get_weather', description: 'Get current weather for a city.', parameters: { type: 'object', properties: { city: { type: 'string' } }, required: ['city'] } } },
  { type: 'function', function: { name: 'calc', description: 'Evaluate a math expression.', parameters: { type: 'object', properties: { expr: { type: 'string' } }, required: ['expr'] } } },
  { type: 'function', function: { name: 'read_file', description: 'Read a file.', parameters: { type: 'object', properties: { path: { type: 'string' } }, required: ['path'] } } },
];

const MODELS = ['mimo-v2.6-flash', 'mimo-v2.6-pro', 'mimo-v2.6-pro-ultraspeed-studio'];

let pass = 0, fail = 0;
const record = (ok, name, detail = '') => {
  console.log(`  ${ok ? '[PASS]' : '[FAIL]'} ${name}${detail ? ' — ' + detail : ''}`);
  ok ? pass++ : fail++;
};

async function chat(model, messages, tools, stream = false) {
  const r = await client.chat.completions.create({
    model, messages, ...(tools ? { tools } : {}), stream,
  });
  if (!stream) {
    const m = r.choices[0].message;
    return { text: m.content || '', toolCalls: m.tool_calls || [], usage: r.usage, finish: r.choices[0].finish_reason };
  }
  let text = '', toolCalls = [], usage = null, finish = null;
  for await (const c of r) {
    if (c.usage) usage = c.usage;
    const ch = c.choices?.[0];
    if (ch?.finish_reason) finish = ch.finish_reason;
    if (ch?.delta?.content) text += ch.delta.content;
    if (ch?.delta?.tool_calls) {
      for (const tc of ch.delta.tool_calls) {
        const i = tc.index ?? 0;
        toolCalls[i] = toolCalls[i] || { id: tc.id, function: { name: '', arguments: '' } };
        if (tc.id) toolCalls[i].id = tc.id;
        if (tc.function?.name) toolCalls[i].function.name += tc.function.name;
        if (tc.function?.arguments) toolCalls[i].function.arguments += tc.function.arguments;
      }
    }
  }
  return { text, toolCalls: toolCalls.filter(Boolean), usage, finish };
}

// ---------- 测试 1：多轮对话上下文保持（会话复用） ----------
async function testMultiTurn(model) {
  console.log(`\n=== [${model}] 多轮对话上下文保持 ===`);
  const facts = [
    { say: '记住三个数字：11、22、33。只回复"记住了"', check: null },
    { say: '第一个数字是多少？只回答数字', check: '11' },
    { say: '第二个和第三个数字连起来是多少？只回答数字', check: '2233' },
    { say: '把三个数字相加，只回答结果', check: '66' },
  ];
  const messages = [{ role: 'system', content: 'You are a helpful assistant. Follow instructions exactly.' }];
  for (let i = 0; i < facts.length; i++) {
    messages.push({ role: 'user', content: facts[i].say });
    try {
      const r = await chat(model, messages, null, false);
      messages.push({ role: 'assistant', content: r.text });
      if (facts[i].check) {
        const ok = r.text.includes(facts[i].check);
        record(ok, `第${i + 1}轮（期望含 ${facts[i].check}）`, JSON.stringify(r.text.slice(0, 50)));
      } else {
        record(r.text.length > 0, `第${i + 1}轮`, JSON.stringify(r.text.slice(0, 40)));
      }
    } catch (e) {
      record(false, `第${i + 1}轮`, e.message.slice(0, 90));
      messages.pop();
    }
  }
}

// ---------- 测试 2：多轮工具调用（agent 循环） ----------
async function testToolLoop(model) {
  console.log(`\n=== [${model}] 多轮工具调用 ===`);
  const messages = [
    { role: 'system', content: 'You are an agent. Use the provided tools when needed. After receiving tool results, answer the user.' },
    { role: 'user', content: '北京现在天气怎么样？用 get_weather 工具查一下。' },
  ];
  let toolRounds = 0;
  for (let step = 0; step < 4; step++) {
    let r;
    try {
      r = await chat(model, messages, TOOLS, false);
    } catch (e) {
      record(false, `step${step + 1}`, e.message.slice(0, 90));
      return;
    }
    if (r.toolCalls.length > 0) {
      toolRounds++;
      messages.push({ role: 'assistant', content: r.text || '', tool_calls: r.toolCalls });
      for (const tc of r.toolCalls) {
        let result = 'unknown tool';
        try {
          const args = JSON.parse(tc.function.arguments || '{}');
          if (tc.function.name === 'get_weather') result = JSON.stringify({ city: args.city || '北京', temp: '18°C', condition: '晴' });
          else if (tc.function.name === 'calc') result = String(eval(args.expr || '0'));
          else if (tc.function.name === 'read_file') result = 'file content';
        } catch { result = 'ok'; }
        messages.push({ role: 'tool', tool_call_id: tc.id, content: result });
      }
      continue;
    }
    record(toolRounds > 0 && r.text.length > 0, `工具调用 ${toolRounds} 轮后给出答案`, JSON.stringify(r.text.slice(0, 70)));
    return;
  }
  record(false, '工具循环', '4 步内未收敛');
}

// ---------- 测试 3：长上下文（首轮大输入 + 后续追问） ----------
async function testLongContext(model) {
  console.log(`\n=== [${model}] 长上下文 ===`);
  const filler = '这是一段用于填充上下文的背景资料，内容本身没有特殊含义。'.repeat(600); // ~15K 字符
  const messages = [
    { role: 'system', content: 'You are an assistant. Read the background carefully and answer questions about it.' },
    { role: 'user', content: `${filler}\n\n背景资料结束。请只回复"已阅读"。` },
  ];
  try {
    const r1 = await chat(model, messages, null, false);
    record(r1.text.length > 0, `首轮 ${filler.length} 字符输入`, JSON.stringify(r1.text.slice(0, 40)));
    messages.push({ role: 'assistant', content: r1.text });
    messages.push({ role: 'user', content: '背景资料里反复出现的那句话是什么？只回答那句话。' });
    const r2 = await chat(model, messages, null, false);
    const ok = r2.text.includes('填充上下文') || r2.text.includes('没有特殊含义');
    record(ok, '长上下文后追问（服务端上下文）', JSON.stringify(r2.text.slice(0, 60)));
  } catch (e) {
    record(false, '长上下文', e.message.slice(0, 90));
  }
}

// ---------- 测试 4：流式多轮 ----------
async function testStreamMultiTurn(model) {
  console.log(`\n=== [${model}] 流式多轮 ===`);
  const messages = [{ role: 'system', content: 'You are concise.' }, { role: 'user', content: '记住数字 888。只回复"好"' }];
  try {
    const r1 = await chat(model, messages, null, true);
    record(r1.text.length > 0 && r1.usage, '流式首轮（含 usage）', `text=${r1.text.length} usage=${r1.usage?.total_tokens}`);
    messages.push({ role: 'assistant', content: r1.text });
    messages.push({ role: 'user', content: '那个数字是什么？只回答数字' });
    const r2 = await chat(model, messages, null, true);
    record(r2.text.includes('888'), '流式第二轮（上下文保持）', JSON.stringify(r2.text.slice(0, 40)));
  } catch (e) {
    record(false, '流式多轮', e.message.slice(0, 90));
  }
}

(async () => {
  console.log(`网关: ${BASE}\n`);
  for (const m of MODELS) {
    await testMultiTurn(m);
    await testToolLoop(m);
    await testLongContext(m);
    await testStreamMultiTurn(m);
  }
  console.log(`\n========== 总计: ${pass} PASS / ${fail} FAIL ==========`);
})();
