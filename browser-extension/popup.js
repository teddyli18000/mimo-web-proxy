document.addEventListener('DOMContentLoaded', () => {
  const syncBtn = document.getElementById('syncBtn');
  const statusEl = document.getElementById('status');
  const previewEl = document.getElementById('preview');
  const gatewayEl = document.getElementById('gateway');
  const apikeyEl = document.getElementById('apikey');

  chrome.storage.local.get(['gateway', 'apikey'], (data) => {
    if (data.gateway) gatewayEl.value = data.gateway;
    if (data.apikey) apikeyEl.value = data.apikey;
  });

  syncBtn.addEventListener('click', async () => {
    const gateway = gatewayEl.value.replace(/\/$/, '');
    const apikey = apikeyEl.value;
    chrome.storage.local.set({ gateway, apikey });

    syncBtn.disabled = true;
    showStatus('loading', '🔍 正在搜索所有 Cookie...');

    try {
      // 宽泛搜索：先列出所有域名的 cookie
      const allCookies = await chrome.cookies.getAll({});
      const debug = [];
      const mimoCookies = {};

      for (const c of allCookies) {
        const d = c.domain;
        // 匹配所有小米/MiMo 相关域名
        if (d.includes('mimo') || d.includes('xiaomi')) {
          debug.push(`${d} | ${c.name} = ${c.value.slice(0, 30)}...`);
          
          if (c.name.includes('serviceToken')) {
            mimoCookies.serviceToken = c.value;
          }
          if (c.name === 'userId' || c.name === 'cUserId') {
            if (!mimoCookies.userId) mimoCookies.userId = c.value;
          }
          if (c.name.includes('_ph')) {
            mimoCookies.ph = c.value;
          }
        }
      }

      // 显示搜索结果
      if (debug.length === 0) {
        showStatus('err', '❌ 未找到任何小米/MiMo Cookie。请确认已登录。');
        previewEl.innerHTML = '<div class="cookie-miss">搜索了全部 ' + allCookies.length + ' 个 Cookie，无匹配</div>';
        previewEl.classList.add('show');
        return;
      }

      showStatus('loading', `📤 找到 ${debug.length} 个相关 Cookie，正在发送...`);

      // 发送到 Gateway（带 API Key 鉴权）
      const resp = await fetch(`${gateway}/admin/api/accounts`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${apikey || 'sk-mimo'}`,
        },
        body: JSON.stringify({
          id: `ext-${Date.now()}`,
          service_token: mimoCookies.serviceToken || '',
          user_id: mimoCookies.userId || '',
          ph: mimoCookies.ph || '',
          active: true,
        }),
      });

      if (resp.ok) {
        showStatus('ok', `✅ 同步成功！找到 ${debug.length} 个 Cookie`);
      } else {
        const txt = await resp.text();
        showStatus('err', `❌ Gateway 错误: ${resp.status} ${txt}`);
      }

      // 显示预览
      const lines = [
        `${mimoCookies.serviceToken ? '✅' : '❌'} serviceToken: ${mimoCookies.serviceToken ? mimoCookies.serviceToken.slice(0, 20) + '...' : 'NOT FOUND'}`,
        `${mimoCookies.userId ? '✅' : '❌'} userId: ${mimoCookies.userId || 'NOT FOUND'}`,
        `${mimoCookies.ph ? '✅' : '❌'} ph: ${mimoCookies.ph ? mimoCookies.ph.slice(0, 20) + '...' : 'NOT FOUND'}`,
      ];
      previewEl.innerHTML = lines.map(l => {
        const cls = l.startsWith('✅') ? 'cookie-ok' : 'cookie-miss';
        return `<div class="${cls}">${l}</div>`;
      }).join('') + `<div style="margin-top:6px;color:#555">--- 全部匹配 ---</div>` + debug.slice(0, 10).map(d => `<div style="color:#555">${d}</div>`).join('');
      previewEl.classList.add('show');

    } catch (e) {
      showStatus('err', `❌ ${e.message}`);
    } finally {
      syncBtn.disabled = false;
    }
  });

  function showStatus(type, msg) {
    statusEl.className = `status ${type}`;
    statusEl.textContent = msg;
  }
});
