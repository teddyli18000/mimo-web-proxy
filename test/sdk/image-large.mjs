// 大图测试：3 MB 真实尺寸图片能否上传并被识别
import fs from 'node:fs';
import OpenAI from 'openai';

const BASE = process.env.GW || 'http://localhost:8090';
const client = new OpenAI({ apiKey: 'sk-mimo', baseURL: BASE });

const big = fs.readFileSync('C:/Users/Teddy/Desktop/dshhhh/_scratch/large-test.png');
console.log(`测试图: ${(big.length / 1024 / 1024).toFixed(2)} MB PNG (1024x1024 噪点)`);

let pass = 0, fail = 0;
const rec = (ok, n, d = '') => { console.log(`  ${ok ? '[PASS]' : '[FAIL]'} ${n}${d ? ' — ' + d : ''}`); ok ? pass++ : fail++; };

(async () => {
  const url = 'data:image/png;base64,' + big.toString('base64');
  console.log(`base64 后长度: ${(url.length / 1024 / 1024).toFixed(2)} MB`);

  const t0 = Date.now();
  try {
    const r = await client.chat.completions.create({
      model: 'mimo-v2.6-flash',
      messages: [{
        role: 'user',
        content: [
          { type: 'text', text: '这张图是纯色还是彩色噪点？只回答"纯色"或"彩色噪点"。' },
          { type: 'image_url', image_url: { url } },
        ],
      }],
    });
    const dt = ((Date.now() - t0) / 1000).toFixed(1);
    const txt = r.choices[0].message.content || '';
    rec(txt.length > 0, `大图请求成功（${dt}s）`, JSON.stringify(txt.slice(0, 60)));
    rec(/噪点|彩色/.test(txt), '模型描述合理', JSON.stringify(txt.slice(0, 50)));
  } catch (e) {
    const dt = ((Date.now() - t0) / 1000).toFixed(1);
    rec(false, `大图请求（${dt}s）`, (e.message || '').slice(0, 150));
  }

  console.log(`\n========== 大图: ${pass} PASS / ${fail} FAIL ==========`);
})();
