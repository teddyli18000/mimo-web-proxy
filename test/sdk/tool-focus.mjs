// 聚焦测试：工具调用可靠性 —— 看模型原生输出什么格式
import OpenAI from 'openai';
const BASE = process.env.GW || 'http://localhost:8090';
const client = new OpenAI({ apiKey: 'sk-mimo', baseURL: BASE });

const TOOLS = [
  { type: 'function', function: { name: 'get_weather', description: 'Get current weather for a city.', parameters: { type: 'object', properties: { city: { type: 'string', description: 'City name' } }, required: ['city'] } } },
  { type: 'function', function: { name: 'run_command', description: 'Run a shell command and return output.', parameters: { type: 'object', properties: { command: { type: 'string' } }, required: ['command'] } } },
];

const SYS = `You are an agent. You MUST use the provided tools to accomplish tasks. Never answer from memory when a tool can provide the answer.

When you need to call a tool, output exactly:
<tool_calls>
<invoke name="TOOL_NAME">
<parameter name="PARAM_NAME">VALUE</parameter>
</invoke>
</tool_calls>`;

async function run(model, userMsg, sys) {
  const r = await client.chat.completions.create({
    model,
    messages: [{ role: 'system', content: sys }, { role: 'user', content: userMsg }],
    tools: TOOLS,
  });
  const m = r.choices[0].message;
  return { text: m.content || '', toolCalls: m.tool_calls || [], finish: r.choices[0].finish_reason };
}

(async () => {
  const cases = [
    { msg: '用 get_weather 工具查一下上海的天气', desc: '明确要求用工具' },
    { msg: '上海现在天气如何？', desc: '隐含需要工具' },
    { msg: '执行命令 echo hello 并告诉我输出', desc: '需要 run_command' },
  ];
  for (const model of ['mimo-v2.6-flash', 'mimo-v2.6-pro', 'mimo-v2.6-pro-ultraspeed-studio']) {
    console.log(`\n===== ${model} =====`);
    for (const c of cases) {
      try {
        const r = await run(model, c.msg, SYS);
        const called = r.toolCalls.length > 0;
        console.log(`  ${called ? '✅' : '❌'} ${c.desc}: tool_calls=${r.toolCalls.length}${called ? ' ' + JSON.stringify(r.toolCalls.map(t => ({ n: t.function.name, a: t.function.arguments }))) : ''}`);
        if (!called) console.log(`      直接回答: ${JSON.stringify(r.text.slice(0, 90))}`);
      } catch (e) {
        console.log(`  ⚠️ ${c.desc}: ${e.message.slice(0, 80)}`);
      }
      await new Promise(r => setTimeout(r, 500));
    }
  }
})();
