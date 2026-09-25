// 长会话稳定性：10+ 轮对话 + 多轮工具调用（模拟真实 agent 长任务）
import OpenAI from 'openai';

const BASE = process.env.GW || 'http://localhost:8090';
const client = new OpenAI({ apiKey: 'sk-mimo', baseURL: BASE });

const TOOLS = [
  { type: 'function', function: { name: 'read', description: 'Read a file.', parameters: { type: 'object', properties: { file_path: { type: 'string' } }, required: ['file_path'] } } },
  { type: 'function', function: { name: 'pwsh', description: 'Run a PowerShell command.', parameters: { type: 'object', properties: { command: { type: 'string' } }, required: ['command'] } } },
];

const SYS = `You are a coding agent working in C:\\work. Use tools to inspect the project.
Available files: src/main.go, src/util.go, README.md.

When calling tools, output:
<tool_calls>
<invoke name="TOOL_NAME">
<parameter name="PARAM_NAME">VALUE</parameter>
</invoke>
</tool_calls>`;

let pass = 0, fail = 0;
const rec = (ok, name, detail = '') => {
  console.log(`  ${ok ? '[PASS]' : '[FAIL]'} ${name}${detail ? ' — ' + detail : ''}`);
  ok ? pass++ : fail++;
};

async function chat(model, messages, stream = false) {
  const r = await client.chat.completions.create({ model, messages, tools: TOOLS, stream });
  if (!stream) {
    const m = r.choices[0].message;
    return { text: m.content || '', toolCalls: m.tool_calls || [], usage: r.usage };
  }
  let text = '', tc = [], usage = null;
  for await (const c of r) {
    if (c.usage) usage = c.usage;
    const ch = c.choices?.[0];
    if (ch?.delta?.content) text += ch.delta.content;
    if (ch?.delta?.tool_calls) {
      for (const t of ch.delta.tool_calls) {
        const i = t.index ?? 0;
        tc[i] = tc[i] || { id: t.id, function: { name: '', arguments: '' } };
        if (t.id) tc[i].id = t.id;
        if (t.function?.name) tc[i].function.name += t.function.name;
        if (t.function?.arguments) tc[i].function.arguments += t.function.arguments;
      }
    }
  }
  return { text, toolCalls: tc.filter(Boolean), usage };
}

async function longSession(model) {
  console.log(`\n===== [${model}] 长会话（12 轮，含工具）=====`);
  const messages = [{ role: 'system', content: SYS }];
  const tasks = [
    '读取 src/main.go 看看入口函数',
    '再读取 src/util.go',
    '这两个文件里有没有重复的函数名？',
    '用 pwsh 执行 go version',
    '把 README.md 也读一下',
    '总结一下这个项目做什么',
    'src/main.go 里第一行是什么？',
    '我一共让你读了几次文件？',
    '用 pwsh 列出 src 目录',
    '这个项目用的什么语言？',
    '把之前读过的文件名都列出来',
    '我们最开始聊的是什么？',
  ];
  let toolRounds = 0, errors = 0;
  for (let i = 0; i < tasks.length; i++) {
    messages.push({ role: 'user', content: tasks[i] });
    try {
      let r = await chat(model, messages);
      let guard = 0;
      while (r.toolCalls.length > 0 && guard < 3) {
        guard++;
        toolRounds++;
        messages.push({ role: 'assistant', content: r.text || '', tool_calls: r.toolCalls });
        for (const tc of r.toolCalls) {
          let result = 'not found';
          try {
            const a = JSON.parse(tc.function.arguments || '{}');
            if (tc.function.name === 'read') result = `// ${a.file_path}\npackage main\nfunc main() {}\nfunc helper() {}`;
            else if (tc.function.name === 'pwsh') result = a.command?.includes('go version') ? 'go version go1.23.4 windows/amd64' : 'main.go  util.go';
          } catch {}
          messages.push({ role: 'tool', tool_call_id: tc.id, content: result });
        }
        r = await chat(model, messages);
      }
      const ok = r.text.length > 0;
      if (!ok) errors++;
      console.log(`    轮${i + 1}: ${ok ? '✅' : '❌'} ${JSON.stringify(r.text.slice(0, 60))}`);
      messages.push({ role: 'assistant', content: r.text });
    } catch (e) {
      errors++;
      console.log(`    轮${i + 1}: ❌ ${e.message.slice(0, 90)}`);
      messages.pop();
    }
  }
  rec(errors === 0, `12 轮全部成功`, `错误=${errors} 工具调用=${toolRounds} 轮`);
  const last = messages[messages.length - 1];
  rec(last.content.includes('读') || last.content.includes('文件') || last.content.includes('main'), '末尾轮次仍记得早期上下文', JSON.stringify(last.content.slice(0, 60)));
}

(async () => {
  for (const m of ['mimo-v2.6-flash', 'mimo-v2.6-pro', 'mimo-v2.6-pro-ultraspeed-studio']) {
    await longSession(m);
  }
  console.log(`\n========== 长会话: ${pass} PASS / ${fail} FAIL ==========`);
})();
