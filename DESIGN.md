# MiMo Gateway — 项目设计文档

## 项目定位

将小米 MiMo AI Studio 网页端转换为低延迟 OpenAI/Anthropic 兼容 API，直接接入本地 Agent（Hermes、Codex、Claude Code、OpenCode 等）。

## 竞品分析

| 项目 | 语言 | ⭐ | 痛点 |
|------|------|-----|------|
| Water008/MiMo2API | Python | 46 | 仅 OpenAI，无多模态路由，延迟高 |
| Fu-Jie/mimo-free-api-mcp | TS | 小 | FC 不稳定，面向 MCP 非 Agent |
| Fly143/MiMo2API | Python | 小 | AI 生成代码质量参差，Python 延迟 |
| **本项目** | **Go** | **目标 1k+** | **低延迟 + 双格式 + 智能路由 + 极致前端** |

## 技术栈

- **后端**: Go 1.22+ / Chi router / SSE 流式
- **前端**: React 18 + TypeScript + Tailwind CSS + Framer Motion
- **构建**: 单二进制（embed 前端到 Go binary）
- **部署**: Docker / 直接运行

## 架构总览

```
┌─────────────────────────────────────────────────┐
│                   Client (Agent)                 │
│   Hermes / Codex / Claude Code / OpenCode        │
└────────────┬──────────────────┬──────────────────┘
             │ OpenAI format    │ Anthropic format
             ▼                  ▼
┌─────────────────────────────────────────────────┐
│              MiMo Gateway (Go)                   │
│  ┌──────────┐  ┌──────────┐  ┌───────────────┐  │
│  │ OpenAI   │  │Anthropic │  │  Management   │  │
│  │ Adapter  │  │ Adapter  │  │  Web UI       │  │
│  └────┬─────┘  └────┬─────┘  └───────────────┘  │
│       │              │                            │
│       ▼              ▼                            │
│  ┌─────────────────────────┐                     │
│  │    Multimodal Router    │                     │
│  │  检测内容 → 选择模型     │                     │
│  └────────────┬────────────┘                     │
│               │                                   │
│  ┌────────────▼────────────┐                     │
│  │    Account Pool         │                     │
│  │  多账号轮转 + 健康检查   │                     │
│  └────────────┬────────────┘                     │
│               │                                   │
│  ┌────────────▼────────────┐                     │
│  │    MiMo Client          │                     │
│  │  SSE 流式代理 + 重试     │                     │
│  └─────────────────────────┘                     │
└────────────────────┬────────────────────────────┘
                     │ Cookie auth
                     ▼
          aistudio.xiaomimimo.com
```

## 多模态路由规则

| 检测到的内容 | 路由模型 | 说明 |
|-------------|---------|------|
| 纯文本 | `mimo-v2.5-pro` | 推理最强 |
| 图片(image_url/base64) | `mimo-v2.5` | 全模态，支持视觉 |
| 音频(audio) | `mimo-v2.5` | 全模态，支持语音 |
| 文件(file) | `mimo-v2.5` | 全模态，支持文档 |
| 显式指定模型 | 尊重用户选择 | 不覆盖 |

## API 端点

### OpenAI 兼容
| 端点 | 方法 | 说明 |
|------|------|------|
| `/v1/chat/completions` | POST | 聊天补全（流式/非流式）|
| `/v1/models` | GET | 模型列表 |
| `/v1/models/{id}` | GET | 模型详情 |

### Anthropic 兼容
| 端点 | 方法 | 说明 |
|------|------|------|
| `/v1/messages` | POST | Anthropic Messages API |
| `/v1/messages/count_tokens` | POST | Token 计数 |

### 管理
| 端点 | 方法 | 说明 |
|------|------|------|
| `/` | GET | 管理前端 SPA |
| `/admin/api/config` | GET/POST | 配置管理 |
| `/admin/api/accounts` | CRUD | 账号管理 |
| `/admin/api/stats` | GET | 统计信息 |
| `/admin/api/health` | GET | 健康检查 |

## 前端设计要求

- 暗色主题，毛玻璃质感
- 流畅动画（Framer Motion）
- 配置 API Key、Base URL
- 模型选择：mimo-v2.5 / mimo-v2.5-pro
- 实时状态仪表盘（请求数、延迟、账号状态）
- 响应式设计

## 项目结构

```
mimo-gateway/
├── DESIGN.md                   # 本文件
├── go.mod
├── go.sum
├── main.go                     # 入口
├── Dockerfile
├── docker-compose.yml
├── .env.example
├── cmd/
│   └── server/
│       └── server.go           # HTTP server 启动
├── internal/
│   ├── config/
│   │   └── config.go           # 配置加载
│   ├── mimo/
│   │   ├── client.go           # MiMo 网页 API 客户端
│   │   ├── auth.go             # Cookie 认证管理
│   │   ├── models.go           # MiMo 数据模型
│   │   └── stream.go           # SSE 流式解析
│   ├── router/
│   │   └── multimodal.go       # 多模态路由决策
│   ├── adapter/
│   │   ├── openai.go           # OpenAI 格式适配
│   │   └── anthropic.go        # Anthropic 格式适配
│   ├── pool/
│   │   └── account.go          # 账号池管理
│   └── handler/
│       ├── chat.go             # /v1/chat/completions
│       ├── messages.go         # /v1/messages
│       ├── models.go           # /v1/models
│       └── admin.go            # 管理接口
├── web/                        # 前端源码
│   ├── package.json
│   ├── tsconfig.json
│   ├── vite.config.ts
│   ├── tailwind.config.ts
│   ├── index.html
│   └── src/
│       ├── main.tsx
│       ├── App.tsx
│       ├── index.css
│       ├── components/
│       │   ├── Layout.tsx
│       │   ├── Sidebar.tsx
│       │   ├── Dashboard.tsx
│       │   ├── ConfigPanel.tsx
│       │   ├── AccountManager.tsx
│       │   ├── ModelSelector.tsx
│       │   ├── StatsCard.tsx
│       │   └── AnimatedBackground.tsx
│       ├── hooks/
│       │   └── useApi.ts
│       ├── lib/
│       │   └── api.ts
│       └── types/
│           └── index.ts
└── static/                     # 前端构建产物（embed 到 Go）
    └── .gitkeep
```

## 开发阶段

### Phase 1：核心后端（先跑通单号）
1. MiMo 客户端（Cookie 认证 + SSE 流式）
2. OpenAI `/v1/chat/completions` 适配
3. 多模态路由
4. 单账号跑通

### Phase 2：前端界面
1. 管理面板框架
2. 配置管理 UI
3. 实时仪表盘
4. 动画和视觉效果

### Phase 3：完善 API
1. Anthropic `/v1/messages` 适配
2. 流式输出开关
3. `/v1/models` 端点

### Phase 4：生产就绪
1. 多账号轮转
2. 健康检查 + 自动刷新 Cookie
3. Docker 部署
4. README + 文档
