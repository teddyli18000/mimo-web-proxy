// 长输出完整性验证：生成大段文本，确认无截断、无丢字
import OpenAI from 'openai';
const client = new OpenAI({ apiKey: 'sk-mimo', baseURL: process.env.GW || 'http://localhost:8080' });

let pass = 0, fail = 0;
const rec = (ok, n, d = '') => { console.log(`  ${ok ? '[PASS]' : '[FAIL]'} ${n}${d ? ' — ' + d : ''}`); ok ? pass++ : fail++; };

async function longOutput(model, stream) {
  const prompt = '写一篇关于"分布式系统一致性"的技术文章，要求：分 5 个小节，每节至少 200 字，包含小标题。直接输出正文，不要客套。';
  const t0 = Date.now();
  try {
    let text = '', usage = null, finish = null;
    if (stream) {
      const s = await client.chat.completions.create({ model, messages: [{ role: 'user', content: prompt }], stream: true });
      for await (const c of s) {
        if (c.usage) usage = c.usage;
        const ch = c.choices?.[0];
        if (ch?.finish_reason) finish = ch.finish_reason;
        if (ch?.delta?.content) text += ch.delta.content;
      }
    } else {
      const r = await client.chat.completions.create({ model, messages: [{ role: 'user', content: prompt }] });
      text = r.choices[0].message.content || '';
      usage = r.usage;
      finish = r.choices[0].finish_reason;
    }
    const dt = ((Date.now() - t0) / 1000).toFixed(1);
    const sections = (text.match(/^#{1,3}\s|^[一二三四五]、|^\d\.\s/gm) || []).length;
    // 完整性检查：结尾不是截断的半句话
    const tail = text.trim().slice(-40);
    const endsClean = /[。！？.!?）)】"]$/.test(text.trim());
    console.log(`  ${model} ${stream ? '流式' : '非流式'}: ${dt}s ${text.length}字 小节=${sections} 结尾干净=${endsClean} usage=${usage?.total_tokens}`);
    console.log(`      结尾: ${JSON.stringify(tail)}`);
    rec(text.length > 800, `${model} ${stream ? '流式' : '非流式'} 输出足够长`, `${text.length} 字`);
    rec(endsClean, `${model} ${stream ? '流式' : '非流式'} 结尾完整`, endsClean ? '' : JSON.stringify(tail));
    rec(!!usage && usage.completion_tokens > 100, `${model} ${stream ? '流式' : '非流式'} usage 合理`, `completion=${usage?.completion_tokens}`);
  } catch (e) {
    rec(false, `${model} ${stream ? '流式' : '非流式'}`, e.message.slice(0, 100));
  }
}

(async () => {
  for (const m of ['mimo-v2.6-flash', 'mimo-v2.6-pro', 'mimo-v2.6-pro-ultraspeed-studio']) {
    await longOutput(m, false);
    await new Promise(r => setTimeout(r, 500));
    await longOutput(m, true);
    await new Promise(r => setTimeout(r, 500));
  }
  console.log(`\n========== 长输出: ${pass} PASS / ${fail} FAIL ==========`);
})();
