// 复现"空响应"：直接用 fetch 发同样的请求，完全掌控原始响应
const BASE = process.env.GW || 'http://localhost:8080';

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

async function chat(model, messages) {
  const r = await fetch(BASE + '/v1/chat/completions', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: 'Bearer sk-mimo' },
    body: JSON.stringify({ model, messages, tools: TOOLS }),
  });
  const raw = await r.text();
  let j = {};
  try { j = JSON.parse(raw); } catch {}
  const m = j.choices?.[0]?.message || {};
  return {
    status: r.status,
    text: m.content || '',
    toolCalls: m.tool_calls || [],
    finish: j.choices?.[0]?.finish_reason,
    raw,
    err: j.error?.message,
  };
}

(async () => {
  const model = process.argv[2] || 'mimo-v2.6-pro';
  const messages = [{ role: 'system', content: SYS }];
  let emptyCount = 0;
  for (let i = 0; i < tasks.length; i++) {
    messages.push({ role: 'user', content: tasks[i] });
    let r = await chat(model, messages);
    let guard = 0;
    while (r.toolCalls.length > 0 && guard < 3) {
      guard++;
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
    const ok = r.text.length > 0 || r.toolCalls.length > 0;
    if (!ok) emptyCount++;
    console.log(`轮${i + 1}: ${ok ? '✅' : '❌'} HTTP=${r.status} finish=${r.finish} text=${r.text.length} tools=${r.toolCalls.length}${r.err ? ' err=' + r.err.slice(0, 80) : ''}`);
    if (!ok) {
      console.log(`  ⚠️ 原始响应体（前 900 字符）:`);
      console.log('  ' + r.raw.slice(0, 900));
    }
    messages.push({ role: 'assistant', content: r.text });
  }
  console.log(`\n空响应数: ${emptyCount}/${tasks.length}`);
})();
