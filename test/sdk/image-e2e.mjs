// 端到端验证图片：过网关上传 → 三个模型都能识图
import OpenAI from 'openai';
import Anthropic from '@anthropic-ai/sdk';
import { solidPng } from './make-png.mjs';

const BASE = process.env.GW || 'http://localhost:8090';
const openai = new OpenAI({ apiKey: 'sk-mimo', baseURL: BASE });
const anthropic = new Anthropic({ apiKey: 'sk-mimo', baseURL: BASE });

const RED = solidPng(128, 128, [220, 30, 30]);
const BLUE = solidPng(128, 128, [30, 60, 220]);
const redURL = 'data:image/png;base64,' + RED.toString('base64');
const blueURL = 'data:image/png;base64,' + BLUE.toString('base64');

let pass = 0, fail = 0;
const rec = (ok, n, d = '') => { console.log(`  ${ok ? '[PASS]' : '[FAIL]'} ${n}${d ? ' — ' + d : ''}`); ok ? pass++ : fail++; };

(async () => {
  console.log('########## A. OpenAI 格式 · 三模型识图 ##########');
  for (const model of ['mimo-v2.6-flash', 'mimo-v2.6-pro', 'mimo-v2.6-pro-ultraspeed-studio']) {
    try {
      const r = await openai.chat.completions.create({
        model,
        messages: [{
          role: 'user',
          content: [
            { type: 'text', text: '这张图是什么颜色？只回答颜色名称。' },
            { type: 'image_url', image_url: { url: redURL } },
          ],
        }],
      });
      const txt = r.choices[0].message.content || '';
      rec(/红/.test(txt), `${model} 识别红色`, JSON.stringify(txt.slice(0, 40)));
    } catch (e) {
      rec(false, `${model} 识图`, e.message.slice(0, 100));
    }
  }

  console.log('\n########## B. 不同图片不串（缓存键正确） ##########');
  try {
    const r = await openai.chat.completions.create({
      model: 'mimo-v2.6-flash',
      messages: [{
        role: 'user',
        content: [
          { type: 'text', text: '这张图是什么颜色？只回答颜色名称。' },
          { type: 'image_url', image_url: { url: blueURL } },
        ],
      }],
    });
    const txt = r.choices[0].message.content || '';
    rec(/蓝/.test(txt), '第二张图识别为蓝色（未命中红色缓存）', JSON.stringify(txt.slice(0, 40)));
  } catch (e) {
    rec(false, '第二张图', e.message.slice(0, 100));
  }

  console.log('\n########## C. 同一张图重复发（走缓存） ##########');
  const t0 = Date.now();
  try {
    const r = await openai.chat.completions.create({
      model: 'mimo-v2.6-flash',
      messages: [{
        role: 'user',
        content: [
          { type: 'text', text: '什么颜色？' },
          { type: 'image_url', image_url: { url: redURL } },
        ],
      }],
    });
    const ms = Date.now() - t0;
    const txt = r.choices[0].message.content || '';
    rec(/红/.test(txt), `重复发送仍正确（${ms}ms）`, JSON.stringify(txt.slice(0, 30)));
  } catch (e) {
    rec(false, '重复发送', e.message.slice(0, 100));
  }

  console.log('\n########## D. Anthropic 格式识图 ##########');
  for (const model of ['mimo-v2.6-flash', 'mimo-v2.6-pro']) {
    try {
      const r = await anthropic.messages.create({
        model, max_tokens: 100,
        messages: [{
          role: 'user',
          content: [
            { type: 'image', source: { type: 'base64', media_type: 'image/png', data: RED.toString('base64') } },
            { type: 'text', text: '这张图是什么颜色？只回答颜色名称。' },
          ],
        }],
      });
      const txt = r.content.filter(b => b.type === 'text').map(b => b.text).join('');
      rec(/红/.test(txt), `Anthropic ${model} 识别红色`, JSON.stringify(txt.slice(0, 40)));
    } catch (e) {
      rec(false, `Anthropic ${model}`, e.message.slice(0, 100));
    }
  }

  console.log('\n########## E. 带图 + 工具（组合场景） ##########');
  try {
    const r = await openai.chat.completions.create({
      model: 'mimo-v2.6-flash',
      messages: [{
        role: 'user',
        content: [
          { type: 'text', text: '用 read_file 读一下 /etc/hostname' },
          { type: 'image_url', image_url: { url: redURL } },
        ],
      }],
      tools: [{ type: 'function', function: { name: 'read_file', description: 'Read a file.', parameters: { type: 'object', properties: { path: { type: 'string' } }, required: ['path'] } } }],
    });
    const m = r.choices[0].message;
    rec((m.tool_calls || []).length > 0 || (m.content || '').length > 0, '带图 + 工具请求正常', `tools=${(m.tool_calls || []).length} text=${(m.content || '').length}`);
  } catch (e) {
    rec(false, '带图 + 工具', e.message.slice(0, 100));
  }

  console.log('\n########## F. 纯文本请求不受影响 ##########');
  try {
    const r = await openai.chat.completions.create({ model: 'mimo-v2.6-flash', messages: [{ role: 'user', content: '只回复 OK' }] });
    rec((r.choices[0].message.content || '').length > 0, '纯文本仍正常', JSON.stringify((r.choices[0].message.content || '').slice(0, 30)));
  } catch (e) {
    rec(false, '纯文本', e.message.slice(0, 100));
  }

  console.log(`\n========== 图片端到端: ${pass} PASS / ${fail} FAIL ==========`);
})();
