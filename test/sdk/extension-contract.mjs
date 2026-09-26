// 模拟浏览器扩展的同步调用（验证扩展与管理接口的契约）
const BASE = process.env.GW || 'http://localhost:8090';
const KEY = 'sk-mimo';

let pass = 0, fail = 0;
const rec = (ok, n, d = '') => { console.log(`  ${ok ? '[PASS]' : '[FAIL]'} ${n}${d ? ' — ' + d : ''}`); ok ? pass++ : fail++; };

async function admin(path, opts = {}) {
  const r = await fetch(BASE + path, {
    ...opts,
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${KEY}`, ...(opts.headers || {}) },
  });
  const text = await r.text();
  let json = null; try { json = JSON.parse(text); } catch {}
  return { status: r.status, json, text };
}

// 扩展发送的就是这个形状
const extPayload = {
  id: `ext-${Date.now()}`,
  service_token: 'dummy-token-for-contract-test',
  user_id: '9999999999',
  ph: 'dummy-ph==',
  active: true,
};

(async () => {
  console.log('########## 扩展 → 管理接口契约 ##########');

  const before = await admin('/admin/api/config');
  const beforeCount = before.json?.accounts?.length ?? 0;
  console.log(`  添加前账号数: ${beforeCount}`);

  const add = await admin('/admin/api/accounts', { method: 'POST', body: JSON.stringify(extPayload) });
  rec(add.status === 200, 'POST /admin/api/accounts 接受扩展的请求体', `status=${add.status} ${JSON.stringify(add.json)}`);

  const after = await admin('/admin/api/config');
  const added = (after.json?.accounts || []).find(a => a.id === extPayload.id);
  rec(!!added, '账号已写入配置', added ? JSON.stringify({ id: added.id, user_id: added.user_id }) : '未找到');
  rec(added?.service_token === extPayload.service_token, 'service_token 字段名匹配（无归一化丢失）');
  rec(added?.user_id === extPayload.user_id, 'user_id 字段名匹配');
  rec(added?.ph === extPayload.ph, 'ph 字段名匹配');

  // 健康检查应覆盖新账号（dummy token 会失败，但不该 500）
  const health = await admin('/admin/api/health');
  rec(health.status === 200, '健康检查在新账号加入后仍正常', JSON.stringify(health.json).slice(0, 80));

  // 无鉴权应被拒
  const noAuth = await fetch(BASE + '/admin/api/accounts', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(extPayload),
  });
  rec(noAuth.status === 401, '扩展漏填 Key 时被拒（不会静默失败）', `status=${noAuth.status}`);

  // 清理
  const del = await admin('/admin/api/accounts', { method: 'DELETE', body: JSON.stringify({ id: extPayload.id }) });
  rec(del.status === 200, 'DELETE 清理测试账号', `status=${del.status}`);
  const final = await admin('/admin/api/config');
  rec((final.json?.accounts?.length ?? -1) === beforeCount, '账号数回到添加前', `${final.json?.accounts?.length}`);

  console.log(`\n========== 扩展契约: ${pass} PASS / ${fail} FAIL ==========`);
})();
