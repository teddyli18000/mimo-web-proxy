<div align="center">

# MiMo Web Proxy

**将小米 MiMo AI Studio 网页端反代为 OpenAI / Anthropic 兼容 API**

Turn the Xiaomi MiMo AI Studio web chat into a standard OpenAI / Anthropic compatible API — one bare `.exe`, no install, no config file needed to start.

Windows 双击即用 · 管理面板内置 · 多账号轮转 · 工具调用 · 会话复用

</div>

---

## English Summary

A local gateway that reverse-proxies [aistudio.xiaomimimo.com](https://aistudio.xiaomimimo.com) into OpenAI (`/v1/chat/completions`) and Anthropic (`/v1/messages`) compatible endpoints, so any OpenAI- or Anthropic-compatible client (Cherry Studio, ChatBox, LobeChat, Claude Code, Cline, DSH, SillyTavern, …) can use MiMo models with your own account cookie.

**Quick start**

1. Download the build for your platform from [Releases](https://github.com/teddyli18000/mimo-web-proxy/releases) — Windows `.zip`, macOS `.dmg`, or Linux `.tar.gz`.
2. Run it. On first start it creates `config.json` and `data/`, then opens the panel at `http://localhost:8080`.
3. Paste your MiMo cookie into the panel (see [获取 MiMo Cookie](#获取-mimo-cookie)), save.
4. Point your client at `http://localhost:8080/v1` with API key `sk-mimo`.

**Features** — session reuse (incremental turns, so long agent sessions stay well under upstream length limits), tool calling for both API formats, thinking-content filtering, multi-account rotation, usage dashboard, zero third-party telemetry.

> ⚠️ Runs on your own account cookie. Personal study and research only. No SLA — the upstream protocol can change at any time.

---

## 这是什么？

把 [aistudio.xiaomimimo.com](https://aistudio.xiaomimimo.com) 网页对话封装成标准 OpenAI / Anthropic API 的本地网关。配好你自己的小米账号 Cookie 后，任何支持 OpenAI 格式的客户端都能调用 MiMo 模型。

> ⚠️ **本项目基于你自己的账号 Cookie 工作，仅供个人学习研究。请勿用于商业用途或大规模调用，后果自负。**

## 快速开始

### 下载即用（三平台）

从 [Releases](https://github.com/teddyli18000/mimo-web-proxy/releases) 下载对应平台的文件：

| 平台 | 文件 | 运行方式 |
|---|---|---|
| **Windows 10/11 (x64)** | `mimo-web-proxy-windows-amd64.zip` | 解压后双击 `mimo-web-proxy.exe` |
| **macOS (Apple Silicon)** | `mimo-web-proxy-macos-arm64.dmg` | 打开 dmg，把 App 拖进「应用程序」，双击运行 |
| **macOS (Intel)** | `mimo-web-proxy-macos-amd64.dmg` | 同上 |
| **Linux (x64)** | `mimo-web-proxy-linux-amd64.tar.gz` | 解压后 `chmod +x mimo-web-proxy && ./mimo-web-proxy` |

程序是**自包含**的：不需要安装、不需要 Node、不需要 Go、不需要预先建配置文件。首次启动自动创建 `config.json` 与 `data/`，并打开面板 `http://localhost:8080`。

**配置与数据位置**（首次启动自动创建）：

| 平台 | 位置 |
|---|---|
| Windows | exe 同目录 |
| macOS | `~/Library/Application Support/mimo-web-proxy/`（`.app` 内不写文件，避免破坏签名） |
| Linux | 二进制同目录 |

> 💡 想换端口或 API Key？改面板「配置」页保存，或直接编辑 `config.json`。想禁止自动开浏览器，设环境变量 `NO_BROWSER_OPEN=1`。

### macOS 首次打开被拦截？

本版本未做代码签名与公证（需要 Apple 开发者账号），Gatekeeper 会提示"无法验证开发者"。**右键点击 App → 打开 → 在弹窗里再点"打开"**，或执行一次：

```bash
xattr -dr com.apple.quarantine "/Applications/MiMo Web Proxy.app"
```

### Linux 依赖

需要 glibc 的发行版（Debian/Ubuntu/Fedora/Arch 均可）。纯静态二进制，无运行时依赖。若系统无桌面环境，设 `NO_BROWSER_OPEN=1` 后手动访问面板。

### 从源码编译

```bash
git clone https://github.com/teddyli18000/mimo-web-proxy.git
cd mimo-web-proxy

# 编译前端（Node 18+）—— static/ 未入库，必须先构建
cd web && npm install && npm run build && cd ..

# 编译后端（Go 1.22+）
go build -o mimo-web-proxy.exe .     # Windows
go build -o mimo-web-proxy .         # macOS / Linux

# 交叉编译示例
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o mimo-web-proxy-mac .
```

### Docker

```bash
docker compose up -d
```

### 自动构建

打 tag 即触发 [GitHub Actions](.github/workflows/release.yml)：三平台并行构建 → 发布 pre-release。

```bash
git tag v1.3.3 && git push origin v1.3.3
```

## 获取 MiMo Cookie

1. 浏览器访问 **https://aistudio.xiaomimimo.com** 并用小米账号登录
2. 在对话框随便发一条消息
3. 按 **F12** 打开开发者工具 → **Network** 标签
4. 找到 `chat` 开头的请求（通常是 `chat?xiaomichatbot_ph=...`）
5. 点开它，在 **Request Headers** 里找到 `cookie:` 字段，提取三个值：

| 配置字段 | Cookie 中的 key | 格式示例 |
|---|---|---|
| `service_token` | `serviceToken`（或 `xiaomichatbot_serviceToken`） | `/Qzv9hyEQZi......`（长 base64，约 390 字符） |
| `user_id` | `userId` | `3215624450......`（纯数字） |
| `ph` | `xiaomichatbot_ph` | `0Fjou2NP2l54M8SRzvNO/g==......` |

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

## 接入客户端

所有 `/v1` 与 `/admin/api` 接口都需要 API Key（默认 `sk-mimo`，**对外使用前请改掉**）。

| 客户端 | Base URL | API Key | 说明 |
|---|---|---|---|
| Cherry Studio / ChatBox / LobeChat / NextChat | `http://localhost:8080/v1` | `sk-mimo` | OpenAI 兼容 |
| Claude Code / Anthropic SDK | `http://localhost:8080` | `sk-mimo` | Anthropic 格式，`/v1/messages` |
| Cline / Roo Code | `http://localhost:8080/v1` | `sk-mimo` | OpenAI 兼容，支持 tools |
| DSH（DeepSeek Harness） | `http://localhost:8080` | `sk-mimo` | `api: openai-completions`，**不带 `/v1`** |
| SillyTavern | `http://localhost:8080/v1` | `sk-mimo` | 选 "OpenAI 兼容" |

> DSH 配置示例（`~/.dsh/settings.yaml`）：
> ```yaml
> providers:
>   mimo-web-local:
>     api: openai-completions
>     baseURL: http://localhost:8080
>     apiKeyEnv: MIMO_WEB_LOCAL_API_KEY
>     models:
>       - { id: mimo-v2.6-pro, name: mimo-v2.6-pro }
>       - { id: mimo-v2.6-flash, name: mimo-v2.6-flash }
>       - { id: mimo-v2.6-pro-ultraspeed-studio, name: mimo-v2.6-pro-ultraspeed-studio }
> ```

### curl 示例

```bash
# OpenAI 格式
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-mimo" \
  -d '{"model":"mimo-v2.6-pro","messages":[{"role":"user","content":"你好"}],"stream":true}'

# Anthropic 格式
curl http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: sk-mimo" \
  -H "anthropic-version: 2023-06-01" \
  -d '{"model":"mimo-v2.6-pro","messages":[{"role":"user","content":"你好"}],"max_tokens":4096}'
```

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

## 特性

- 🔧 **工具调用** — OpenAI / Anthropic 双格式 tools，注入格式说明 + DSML/XML 解析，三模型实测可用
- 🔥 **双格式兼容** — `/v1/chat/completions` 与 `/v1/messages`（含流式）
- 🔁 **会话复用（增量发送）** — 首轮发完整上下文，后续轮次只发新消息，依赖上游服务端上下文。与网页端行为一致：实测 12 轮 agent 会话平均 query 仅 240 字符（全量重放需 70K+），长任务不再撞长度限制
- 🧠 **思考内容自动过滤** — thinking 段不会漏进正文
- 🖼️ **多模态智能路由** — 按内容类型自动选模型
- 👥 **多账号轮转** — 配多个账号自动分担负载
- 📊 **管理面板** — 用量统计、模型分布、账号管理、健康检查
- 🔒 **无第三方遥测** — 面板与扩展零外联，凭证只存本机 `config.json`
- ⚡ **单二进制** — Go 编译，前端内嵌，双击即用

## 会话与上下文

网关按「消息指纹前缀延续」判断客户端请求是不是同一会话的下一轮：

| 场景 | 行为 |
|---|---|
| 新会话（首轮） | 发送完整上下文（system + 历史 + 当前消息）+ 完整工具 schema |
| 延续会话 | **只发新增消息** + 紧凑工具名提醒，其余交给上游服务端上下文 |
| 客户端重放同一历史 | 判定为新会话，发完整上下文（不会误用已推进的旧会话） |
| 上游返回空响应 | 换新会话并用**完整上下文**重试一次 |
| 上游 429 / 5xx | 换新 msgId 退避重试（2s→16s 带抖动，最多 5 次） |

本轮提问的识别：agent 客户端（DSH、Cline 等）把用户提问放在本轮消息的**第一条**，注入上下文（AGENTS.md、技能目录、运行时信息）跟在后面。网关取本轮第一条 user 消息作为提问，其余渲染为 `[Context]` 放在提问之前。

## 配置说明

`config.json`（可用 `CONFIG_PATH` 环境变量改路径）：

| 字段 | 说明 | 默认 |
|------|------|------|
| `port` | 监听端口 | `8080` |
| `api_key` | API/管理接口认证密钥 | `sk-mimo` |
| `default_model` | 默认模型 | `mimo-v2.6-pro` |
| `accounts` | 账号池（多账号自动轮转） | `[]` |

环境变量（可选）：`PORT`、`API_KEY`、`CONFIG_PATH`、`NO_BROWSER_OPEN=1`（禁止启动时自动开浏览器）。

## 管理接口

| 端点 | 说明 |
|---|---|
| `GET /admin/api/config` | 读取配置 |
| `POST /admin/api/config` | 更新默认模型 |
| `POST/DELETE /admin/api/accounts` | 增删账号 |
| `GET /admin/api/health` | 账号健康检查 |
| `GET /admin/api/stats` | 用量统计 |

面板首次使用时在设置里填一次 API Key（存于浏览器 localStorage，只在你本机）。

## 测试

仓库内 [`test/sdk/`](test/sdk/README.md) 是用**官方 SDK**（`openai`、`@anthropic-ai/sdk`）对真实上游跑的集成测试——用与真实客户端相同的协议栈验证，能发现手写 HTTP 断言测不出的协议层问题。

```bash
cd test/sdk && npm install

npm run stability     # 三模型 × 多轮上下文 / 工具调用 / 长上下文 / 流式
npm run anthropic     # Anthropic 格式：基础 / 多轮 / tool_use / 流式
npm run long-session  # 12 轮长会话，验证末尾仍记得首轮内容
npm run long-output   # 3000+ 字长输出完整性
npm run tools         # 工具调用可靠性
```

Go 单元测试：`go test ./...`

## 安全说明

- 管理接口与 API 接口共用同一 API Key 鉴权，**请改掉默认 `sk-mimo`**
- Cookie 凭证以明文存于 `config.json`（`.gitignore` 已排除），**不要把该文件发给别人**
- 本项目不引入任何第三方遥测；管理面板与浏览器扩展不向外部发送数据
- 上游可能随时改协议或加风控，本项目不保证长期可用

## 常见问题

**Q: Cookie 多久过期？** 数天到数周不等，面板健康检查变红就重抓。

**Q: 请求 401 / 账号风险？** Cookie 过期或被上游风控，重抓 Cookie；仍不行则换账号。

**Q: 和官方 API 的区别？** 官方 API 有付费额度限制；本代理走网页端免费额度，但无 SLA。

**Q: 支持流式吗？** 支持，`"stream": true` 即可。

**Q: 工具调用一直不触发？** 上游网页端没有原生 tools 参数，网关靠注入提示词约定调用格式。模型偶尔会选择直接回答而不调工具（尤其是问题看起来能凭记忆回答时）。客户端 system prompt 里明确要求使用工具会显著提高触发率。

## 致谢与许可

本项目基于 [wtz44/mimo-free-api](https://github.com/wtz44/mimo-free-api)（MIT）修改，模型协议参考了社区公开的逆向实现（[meny2333/mimo2api_go](https://github.com/meny2333/mimo2api_go)、[Fu-Jie/mimo-free-api-mcp](https://github.com/Fu-Jie/mimo-free-api-mcp) 等）的设计思路。感谢原作者的开创性工作。

[MIT](LICENSE) © 2026 teddyli18000
