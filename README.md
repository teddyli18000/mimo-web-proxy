<div align="center">

# MiMo Web Proxy

**将小米 MiMo AI Studio 网页端反代为 OpenAI / Anthropic 兼容 API**

Windows 双击即用 · 管理面板内置 · 多账号轮转 · 工具调用支持

</div>

---

## 这是什么？

把 [aistudio.xiaomimimo.com](https://aistudio.xiaomimimo.com) 网页对话封装成标准 OpenAI / Anthropic API 的本地网关。配好你自己的小米账号 Cookie 后，任何支持 OpenAI 格式的客户端（Cherry Studio、ChatBox、LobeChat、Claude Code、酒馆等）都能免费调用 MiMo 模型。

> ⚠️ **本项目基于你自己的账号 Cookie 工作，仅供个人学习研究。请勿用于商业用途或大规模调用，后果自负。**

## 快速开始

### 方式一：Windows 双击运行（推荐）

1. 从 [Releases](https://github.com/teddyli18000/mimo-web-proxy/releases) 下载 `mimo-web-proxy-windows-amd64.zip`
2. 解压到任意目录，双击 `mimo-web-proxy.exe`
3. 浏览器自动打开管理面板 `http://localhost:8080`
4. 按 [获取 Cookie](#获取-mimo-cookie) 填入账号，保存后即可调用

首次启动会生成 `config.json` 和 `data/` 目录（与 exe 同目录）。

### 方式二：源码编译

```bash
git clone https://github.com/teddyli18000/mimo-web-proxy.git
cd mimo-web-proxy

# 编译前端（Node 18+）
cd web && npm install && npm run build && cd ..

# 编译后端（Go 1.22+）
go build -o mimo-web-proxy.exe .
```

### 方式三：Docker

```bash
docker compose up -d
```

## 获取 MiMo Cookie

1. 浏览器访问 **https://aistudio.xiaomimimo.com** 并用小米账号登录
2. 在对话框随便发一条消息
3. 按 **F12** 打开开发者工具 → **Network** 标签
4. 找到 `chat` 开头的请求（通常是 `chat?xiaomichatbot_ph=...`）
5. 点开它，在 **Request Headers** 里找到 `cookie:` 字段，提取三个值：

| 配置字段 | Cookie 中的 key | 格式示例 |
|---|---|---|
| `service_token` | `serviceToken` | `/Qzv9hyEQZi...`（长 base64） |
| `user_id` | `userId` | `3215624450`（纯数字） |
| `ph` | `xiaomichatbot_ph` | `0Fjou2NP2l54M8SRzvNO/g==` |

6. 填入管理面板「配置 → 账号池 → 添加」，或直接编辑 `config.json`：

```json
{
  "accounts": [
    {
      "id": "my-account",
      "service_token": "...",
      "user_id": "...",
      "ph": "...",
      "active": true
    }
  ]
}
```

> 💡 也可以用 `browser-extension/` 目录里的浏览器扩展一键抓取（只发往你本机网关）。
> Cookie 有有效期，过期后面板健康检查会显示异常，重新抓一次即可。

## 模型

上游实时的模型列表可访问 `GET /v1/models`。

| 模型 ID | 说明 |
|---|---|
| `mimo-v2.6-flash` | v2.6 日常全模态（300B，1M 上下文） |
| `mimo-v2.6-pro` | v2.6 深度推理（1T，1M 上下文） |
| `mimo-v2.6-pro-ultraspeed-studio` | v2.6 极速版 |
| `mimo-v2.5` / `mimo-v2.5-pro` | 上一代（兼容保留） |

- 显式指定 `model` → 按指定模型调用（含别名归一化）
- 不指定或 `auto` → 含图片/音频/文件自动用 `mimo-v2.6-flash`，纯文本用 `mimo-v2.6-pro`

## API 用法

所有 `/v1` 与 `/admin/api` 接口均需 API Key（默认 `sk-mimo`，在 `config.json` 修改，**改掉默认值再对外用**）。

### OpenAI 格式

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-mimo" \
  -d '{"model":"mimo-v2.6-pro","messages":[{"role":"user","content":"你好"}],"stream":true}'
```

### Anthropic 格式

```bash
curl http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: sk-mimo" \
  -H "anthropic-version: 2023-06-01" \
  -d '{"model":"mimo-v2.6-pro","messages":[{"role":"user","content":"你好"}],"max_tokens":4096}'
```

### 第三方客户端接入

| 客户端 | Base URL | API Key |
|--------|----------|---------|
| Cherry Studio / ChatBox / ChatGPT-Next-Web | `http://localhost:8080/v1` | `sk-mimo` |
| Claude Desktop / Anthropic SDK | `http://localhost:8080` | `sk-mimo` |

### 管理接口

管理接口同样需要 API Key。面板首次使用前在设置里填一次 Key（存于浏览器 localStorage，只存你本机）。

| 端点 | 说明 |
|---|---|
| `GET /admin/api/config` | 读取配置 |
| `POST /admin/api/config` | 更新默认模型 |
| `POST/DELETE /admin/api/accounts` | 增删账号 |
| `GET /admin/api/health` | 账号健康检查 |
| `GET /admin/api/stats` | 用量统计 |

## 配置说明

`config.json`（可用 `CONFIG_PATH` 环境变量改路径）：

| 字段 | 说明 | 默认 |
|------|------|------|
| `port` | 监听端口 | `8080` |
| `api_key` | API/管理接口认证密钥 | `sk-mimo` |
| `default_model` | 默认模型 | `mimo-v2.6-pro` |
| `accounts` | 账号池（多账号自动轮转） | `[]` |

环境变量（可选）：`PORT`、`API_KEY`、`CONFIG_PATH`、`NO_BROWSER_OPEN=1`（禁止启动时自动开浏览器）。

## 特性

- 🔧 **工具调用** — OpenAI / Anthropic 双格式 tools，注入提示 + DSML/XML 解析
- 🔥 **双格式兼容** — `/v1/chat/completions` 与 `/v1/messages`
- 🧠 **思考内容自动过滤** — thinking 段不会漏进正文
- 🖼️ **多模态智能路由** — 按内容类型自动选模型
- 👥 **多账号轮转** — 配多个账号自动分担负载
- 📊 **管理面板** — 用量统计、模型分布、账号管理、健康检查
- 🔒 **无第三方遥测** — 面板与扩展零外联，凭证只存本机 `config.json`
- ⚡ **单二进制** — Go 编译，前端内嵌，双击即用

## 安全说明

- 管理接口与 API 接口共用同一 API Key 鉴权，**请改掉默认 `sk-mimo`**
- Cookie 凭证以明文存于 `config.json`（`.gitignore` 已排除），**不要把该文件发给别人**
- 上游可能随时改协议或加风控，本项目不保证长期可用

## 常见问题

**Q: Cookie 多久过期？** 数天到数周不等，面板健康检查变红就重抓。

**Q: 请求 401 / 账号风险？** Cookie 过期或被上游风控，重抓 Cookie；仍不行则换账号。

**Q: 和官方 API 的区别？** 官方 API 有付费额度限制；本代理走网页端免费额度，但无 SLA。

**Q: 支持流式吗？** 支持，`"stream": true` 即可。

## 致谢与许可

本项目基于 [wtz44/mimo-free-api](https://github.com/wtz44/mimo-free-api)（MIT）修改，模型协议参考了社区公开的逆向实现。感谢原作者的开创性工作。

[MIT](LICENSE) © 2026 teddyli18000
