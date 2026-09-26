// 验证：工具调用时模型的说明文字是否保留（OpenAI 允许 content 与 tool_calls 并存）
import OpenAI from 'openai';

const BASE = process.env.GW || 'http://localhost:8090';
const client = new OpenAI({ apiKey: 'sk-mimo', baseURL: BASE });

const TOOLS = [
  { type: 'function', function: { name: 'read_file', description: 'Read a file.', parameters: { type: 'object', properties: { path: { type: 'string' } }, required: ['path'] } } },
];

let pass = 0, fail = 0;
const rec = (ok, n, d = '') => { console.log(`  ${ok ? '[PASS]' : '[FAIL]'} ${n}${d ? ' — ' + d : ''}`); ok ? pass++ : fail++; };
const PROMPT = '先说明你为什么需要读文件，然后再调用 read_file 工具读 /etc/hostname。';

(async () => {
  console.log('########## 非流式 ##########');
  for (const model of ['mimo-v2.6-flash', 'mimo-v2.6-pro']) {
    try {
      const r = await client.chat.completions.create({
        model, messages: [{ role: 'user', content: PROMPT }], tools: TOOLS,
      });
      const m = r.choices[0].message;
      const hasCall = (m.tool_calls || []).length > 0;
      const text = m.content || '';
      console.log(`  ${model}: tool_calls=${m.tool_calls?.length || 0} content=${text.length} 字`);
      if (hasCall) {
        rec(text.length > 0, `${model} 调用工具时保留了说明文字`, JSON.stringify(text.slice(0, 60)));
        rec(!/<tool_calls>|<invoke name|<\/?function_calls/.test(text), `${model} 说明文字里没有裸语法`, JSON.stringify(text.slice(0, 60)));
      } else {
        rec(false, `${model} 未调用工具（模型选择直接回答）`, JSON.stringify(text.slice(0, 60)));
      }
    } catch (e) {
      rec(false, `${model}`, e.message.slice(0, 100));
    }
  }

  console.log('\n########## 流式 ##########');
  for (const model of ['mimo-v2.6-flash', 'mimo-v2.6-pro']) {
    try {
      const stream = await client.chat.completions.create({
        model, messages: [{ role: 'user', content: PROMPT }], tools: TOOLS, stream: true,
      });
      let text = '', calls = [], finish = null;
      for await (const c of stream) {
        const ch = c.choices?.[0];
        if (ch?.finish_reason) finish = ch.finish_reason;
        if (ch?.delta?.content) text += ch.delta.content;
        if (ch?.delta?.tool_calls) {
          for (const t of ch.delta.tool_calls) {
            const i = t.index ?? 0;
            calls[i] = calls[i] || { name: '', args: '' };
            if (t.function?.name) calls[i].name += t.function.name;
            if (t.function?.arguments) calls[i].args += t.function.arguments;
          }
        }
      }
      const hasCall = calls.filter(Boolean).length > 0;
      console.log(`  ${model}: tool_calls=${calls.filter(Boolean).length} content=${text.length} 字 finish=${finish}`);
      if (hasCall) {
        rec(text.length > 0, `${model} 流式保留说明文字`, JSON.stringify(text.slice(0, 60)));
        rec(finish === 'tool_calls', `${model} finish_reason=tool_calls`, String(finish));
      } else {
        rec(false, `${model} 流式未调用工具`, JSON.stringify(text.slice(0, 50)));
      }
    } catch (e) {
      rec(false, `${model} 流式`, e.message.slice(0, 100));
    }
  }

  console.log(`\n========== 说明文字保留: ${pass} PASS / ${fail} FAIL ==========`);
})();
