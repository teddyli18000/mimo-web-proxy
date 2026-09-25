// 从用户附上的失败会话提取请求，回放验证修复效果
import fs from 'node:fs';
import OpenAI from 'openai';

const src = 'C:/Users/Teddy/.dsh/attachments/v1/files/66/668347adc8dc3b462547a5f373696ccd958c3bbd530051d52cdb31dc82540de2/session.v4.jsonl';
const lines = fs.readFileSync(src, 'utf8').split('\n').filter(Boolean);

let header = null;
const messages = [];
const text = (c) => typeof c === 'string' ? c : (Array.isArray(c) ? c.map(x => x.text || '').join('') : JSON.stringify(c));

for (const line of lines) {
  let e; try { e = JSON.parse(line); } catch { continue; }
  const d = e.data || {};
  if (e.type === 'request/header') { header = d.header; continue; }
  if (e.type === 'system/message' && d.message) messages.push({ role: 'system', content: text(d.message.content) });
  if (e.type === 'user/message') {
    const c = d.content ?? d.message?.content;
    if (c) messages.push({ role: 'user', content: text(c) });
  }
}

const tools = (header?.tools || []).map(t => ({ type: 'function', function: { name: t.name, description: t.description, parameters: t.parameters } }));
console.log(`提取请求: model=${header?.config?.model} tools=${tools.length} messages=${messages.length}`);
messages.forEach((m, i) => console.log(`  [${i}] ${m.role} ${m.content.length} 字符: ${JSON.stringify(m.content.slice(0, 70))}`));

const BASE = process.env.GW || 'http://localhost:8080';
const client = new OpenAI({ apiKey: 'sk-mimo', baseURL: BASE });

(async () => {
  console.log(`\n回放到 ${BASE} ...`);
  const t0 = Date.now();
  const r = await client.chat.completions.create({ model: header?.config?.model || 'mimo-v2.6-pro-ultraspeed-studio', messages, tools });
  const m = r.choices[0].message;
  const body = m.content || '';
  const tc = m.tool_calls || [];
  console.log(`\n耗时 ${((Date.now() - t0) / 1000).toFixed(1)}s | 正文 ${body.length} 字 | tool_calls ${tc.length}`);
  console.log(`回复: ${JSON.stringify(body.slice(0, 300))}`);
  if (tc.length) console.log(`工具调用: ${JSON.stringify(tc.map(t => ({ n: t.function.name, a: (t.function.arguments || '').slice(0, 120) })))}`);

  // 判定：用户只说"你好"，正确行为是简短问候，不该写脚本
  const isGreeting = /你好|您好|Hi|Hello|有什么|可以帮/.test(body);
  const wroteScript = tc.some(t => t.function.name === 'write') || /quiz_runner|studyvault/i.test(body);
  console.log(`\n判定: ${isGreeting && !wroteScript ? '✅ 正确回应问候' : wroteScript ? '❌ 仍在写脚本（未修复）' : '⚠️ 需人工确认'}`);
})();
