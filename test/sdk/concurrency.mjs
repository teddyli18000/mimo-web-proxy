// 并发、会话隔离与资源增长测试
const BASE = process.env.GW || 'http://localhost:8090';
const KEY = 'sk-mimo';

let pass = 0, fail = 0;
const rec = (ok, n, d = '') => { console.log(`  ${ok ? '[PASS]' : '[FAIL]'} ${n}${d ? ' — ' + d : ''}`); ok ? pass++ : fail++; };

async function chat(messages, model = 'mimo-v2.6-flash') {
  const t0 = Date.now();
  const r = await fetch(BASE + '/v1/chat/completions', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${KEY}` },
    body: JSON.stringify({ model, messages }),
  });
  const j = await r.json().catch(() => ({}));
  return { status: r.status, text: j.choices?.[0]?.message?.content || '', err: j.error?.message, ms: Date.now() - t0 };
}

(async () => {
  console.log('########## A. 并发 8 路独立请求 ##########');
  const N = 8;
  const t0 = Date.now();
  const results = await Promise.all(
    Array.from({ length: N }, (_, i) =>
      chat([{ role: 'user', content: `记住数字 ${1000 + i}，只回复"好"` }])
    )
  );
  const okCount = results.filter(r => r.status === 200 && r.text.length > 0).length;
  rec(okCount === N, `${N} 路并发全部成功`, `${okCount}/${N}，总耗时 ${((Date.now() - t0) / 1000).toFixed(1)}s`);
  const errs = results.filter(r => r.err).map(r => r.err.slice(0, 60));
  if (errs.length) console.log('       错误:', errs.join(' | '));

  console.log('\n########## B. 并发下的会话隔离（上下文不串） ##########');
  // 两个会话各记住不同数字，第二轮各自追问，确认拿到自己的数字
  const [a1, b1] = await Promise.all([
    chat([{ role: 'user', content: '记住数字 4271，只回复"好"' }]),
    chat([{ role: 'user', content: '记住数字 9138，只回复"好"' }]),
  ]);
  const [a2, b2] = await Promise.all([
    chat([
      { role: 'user', content: '记住数字 4271，只回复"好"' },
      { role: 'assistant', content: a1.text },
      { role: 'user', content: '我让你记的数字是多少？只回答数字' },
    ]),
    chat([
      { role: 'user', content: '记住数字 9138，只回复"好"' },
      { role: 'assistant', content: b1.text },
      { role: 'user', content: '我让你记的数字是多少？只回答数字' },
    ]),
  ]);
  const aOk = a2.text.includes('4271') && !a2.text.includes('9138');
  const bOk = b2.text.includes('9138') && !b2.text.includes('4271');
  rec(aOk, '会话 A 拿到自己的数字 4271', JSON.stringify(a2.text.slice(0, 40)));
  rec(bOk, '会话 B 拿到自己的数字 9138', JSON.stringify(b2.text.slice(0, 40)));

  console.log('\n########## C. 串行 10 轮同一会话（上下文不丢） ##########');
  const msgs = [{ role: 'user', content: '记住数字 5555，只回复"好"' }];
  let last = '';
  let errCount = 0;
  for (let i = 0; i < 10; i++) {
    const q = i === 9 ? '那个数字是多少？只回答数字' : `第${i + 1}轮确认，只回复"收到"`;
    msgs.push({ role: 'user', content: q });
    const r = await chat(msgs);
    if (r.status !== 200 || !r.text) { errCount++; break; }
    msgs.push({ role: 'assistant', content: r.text });
    last = r.text;
  }
  rec(errCount === 0, '10 轮串行无错误', `错误=${errCount}`);
  rec(last.includes('5555'), '第 10 轮仍记得 5555', JSON.stringify(last.slice(0, 40)));

  console.log('\n########## D. 混合负载（流式 + 非流式并发） ##########');
  const mixed = await Promise.all([
    fetch(BASE + '/v1/chat/completions', {
      method: 'POST', headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${KEY}` },
      body: JSON.stringify({ model: 'mimo-v2.6-flash', messages: [{ role: 'user', content: '只回复 OK' }], stream: true }),
    }).then(r => r.text()),
    chat([{ role: 'user', content: '只回复 OK' }]),
    fetch(BASE + '/v1/messages', {
      method: 'POST', headers: { 'Content-Type': 'application/json', 'x-api-key': KEY, 'anthropic-version': '2023-06-01' },
      body: JSON.stringify({ model: 'mimo-v2.6-flash', max_tokens: 100, messages: [{ role: 'user', content: '只回复 OK' }] }),
    }).then(r => r.json()),
  ]);
  const streamOk = typeof mixed[0] === 'string' && mixed[0].includes('[DONE]');
  const nonStreamOk = mixed[1].status === 200;
  const anthropicOk = Array.isArray(mixed[2].content);
  rec(streamOk, '并发流式正常（含 [DONE]）');
  rec(nonStreamOk, '并发非流式正常');
  rec(anthropicOk, '并发 Anthropic 正常');

  console.log(`\n========== 并发测试: ${pass} PASS / ${fail} FAIL ==========`);
})();
