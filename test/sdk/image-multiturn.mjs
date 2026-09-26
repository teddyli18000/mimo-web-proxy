// 多轮图片：第一轮发图，后续轮不再发图，模型能否继续看到
import OpenAI from 'openai';
import { solidPng } from './make-png.mjs';

const BASE = process.env.GW || 'http://localhost:8090';
const client = new OpenAI({ apiKey: 'sk-mimo', baseURL: BASE });

const RED = solidPng(128, 128, [220, 30, 30]);
const BLUE = solidPng(128, 128, [30, 60, 220]);
const dataURL = (b) => 'data:image/png;base64,' + b.toString('base64');

let pass = 0, fail = 0;
const rec = (ok, n, d = '') => { console.log(`  ${ok ? '[PASS]' : '[FAIL]'} ${n}${d ? ' — ' + d : ''}`); ok ? pass++ : fail++; };

async function chat(messages, model = 'mimo-v2.6-flash') {
  const r = await client.chat.completions.create({ model, messages });
  return r.choices[0].message.content || '';
}

(async () => {
  for (const model of ['mimo-v2.6-flash', 'mimo-v2.6-pro']) {
    console.log(`\n===== ${model} =====`);
    const msgs = [];

    // 第 1 轮：带图提问
    msgs.push({
      role: 'user',
      content: [
        { type: 'text', text: '这张图是什么颜色？只回答颜色名称。' },
        { type: 'image_url', image_url: { url: dataURL(RED) } },
      ],
    });
    let r1;
    try { r1 = await chat(msgs, model); } catch (e) { rec(false, '第 1 轮', e.message.slice(0, 90)); continue; }
    msgs.push({ role: 'assistant', content: r1 });
    rec(/红/.test(r1), '第 1 轮识图', JSON.stringify(r1.slice(0, 40)));

    // 第 2 轮：不带图，追问图片细节（增量发送，服务端应保有图片）
    msgs.push({ role: 'user', content: '我刚才发给你的那张图，是纯色还是有花纹？只回答"纯色"或"有花纹"。' });
    let r2;
    try { r2 = await chat(msgs, model); } catch (e) { rec(false, '第 2 轮', e.message.slice(0, 90)); continue; }
    msgs.push({ role: 'assistant', content: r2 });
    const rememberImg = /纯色/.test(r2);
    rec(rememberImg, '第 2 轮仍记得图片内容', JSON.stringify(r2.slice(0, 50)));

    // 第 3 轮：再换一张新图（同会话），确认新图覆盖旧图
    msgs.push({
      role: 'user',
      content: [
        { type: 'text', text: '现在这张新图是什么颜色？只回答颜色名称。' },
        { type: 'image_url', image_url: { url: dataURL(BLUE) } },
      ],
    });
    let r3;
    try { r3 = await chat(msgs, model); } catch (e) { rec(false, '第 3 轮', e.message.slice(0, 90)); continue; }
    msgs.push({ role: 'assistant', content: r3 });
    rec(/蓝/.test(r3), '第 3 轮识别新图', JSON.stringify(r3.slice(0, 40)));

    // 第 4 轮：问最初那张的颜色（验证历史里的两张图没串）
    msgs.push({ role: 'user', content: '我最开始发的那张图是什么颜色？只回答颜色名称。' });
    let r4;
    try { r4 = await chat(msgs, model); } catch (e) { rec(false, '第 4 轮', e.message.slice(0, 90)); continue; }
    const firstOk = /红/.test(r4);
    rec(firstOk, '第 4 轮记得第一张图是红色', JSON.stringify(r4.slice(0, 50)));
  }

  console.log(`\n========== 多轮图片: ${pass} PASS / ${fail} FAIL ==========`);
})();
