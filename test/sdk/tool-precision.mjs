// 验证新提示词：该调工具时调、不该调时不乱调（双向验证，防过度调用）
import OpenAI from 'openai';

const BASE = process.env.GW || 'http://localhost:8090';
const client = new OpenAI({ apiKey: 'sk-mimo', baseURL: BASE });

const TOOLS = [
  { type: 'function', function: { name: 'get_weather', description: 'Get current weather for a city.', parameters: { type: 'object', properties: { city: { type: 'string' } }, required: ['city'] } } },
  { type: 'function', function: { name: 'run_command', description: 'Run a shell command.', parameters: { type: 'object', properties: { command: { type: 'string' } }, required: ['command'] } } },
  { type: 'function', function: { name: 'read_file', description: 'Read a file.', parameters: { type: 'object', properties: { path: { type: 'string' } }, required: ['path'] } } },
];

// 应该调用工具
const SHOULD_CALL = [
  '上海现在天气如何？',
  '用 get_weather 查一下北京天气',
  '执行命令 echo hello 给我看输出',
  '帮我读一下 /tmp/a.txt',
  '东京今天多少度？',
];
// 不该调用工具
const SHOULD_NOT_CALL = [
  '你好',
  '解释一下什么是递归',
  '把"今天天气不错"翻译成英文',
  '1+1 等于几？',
];

async function run(model, q) {
  const r = await client.chat.completions.create({
    model,
    messages: [{ role: 'system', content: 'You are a helpful assistant.' }, { role: 'user', content: q }],
    tools: TOOLS,
  });
  const m = r.choices[0].message;
  return { called: (m.tool_calls || []).length > 0, text: m.content || '' };
}

(async () => {
  let tp = 0, tn = 0, fp = 0, fn = 0;
  for (const model of ['mimo-v2.6-pro', 'mimo-v2.6-flash', 'mimo-v2.6-pro-ultraspeed-studio']) {
    console.log(`\n===== ${model} =====`);
    for (const q of SHOULD_CALL) {
      const r = await run(model, q);
      r.called ? tp++ : fn++;
      console.log(`  ${r.called ? '✅' : '❌ 漏调'} [应调] ${q}${r.called ? '' : ' → ' + JSON.stringify(r.text.slice(0, 50))}`);
      await new Promise(x => setTimeout(x, 400));
    }
    for (const q of SHOULD_NOT_CALL) {
      const r = await run(model, q);
      r.called ? fp++ : tn++;
      console.log(`  ${r.called ? '⚠️ 多调' : '✅'} [不应调] ${q}`);
      await new Promise(x => setTimeout(x, 400));
    }
  }
  console.log(`\n===== 汇总 =====`);
  console.log(`  该调就调: ${tp}/${tp + fn}`);
  console.log(`  不该调不调: ${tn}/${tn + fp}`);
  console.log(`  误报(多调): ${fp} | 漏报(漏调): ${fn}`);
})();
