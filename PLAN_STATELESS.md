# mimo-gateway 无状态重构计划

## 目标
参照 ds2api 的无状态架构，重写 mimo-gateway 的消息规范化和 prompt 构建，
使模型每次都能看到完整的对话上下文（含 tool_calls 历史和 tool_result）。

## 架构

```
MiMo Code (OpenAI/Anthropic messages[])
  ↓
[NormalizeMessages] — 按 role 规范化每条消息
  ↓
[InjectToolPrompt] — 工具 schema + DSML 指令注入 system message
  ↓
[BuildPrompt] — 用特殊 token 组装最终 prompt 字符串
  ↓
MiMo Web API (单条 query)
```

## 任务清单

### Task 1: 创建 `internal/prompt/` 包
- `messages.go` — 特殊 token 常量 + prompt 组装
- `tool_calls.go` — tool_calls 渲染为 DSML XML

### Task 2: 创建 `internal/promptcompat/` 包
- `message_normalize.go` — OpenAI/Anthropic 消息规范化
- `tool_prompt.go` — 工具 schema 注入到 system message
- `prompt_build.go` — 编排：normalize → inject → build

### Task 3: 重写 handler
- `chat.go` ChatHandler: 用新 pipeline 替换 buildQuery
- `chat.go` MessagesHandler: 用新 pipeline 替换 buildAnthropicQuery
- 删除旧的 buildQuery/buildAnthropicQuery/extractAnthropicContent 等函数

### Task 4: 清理
- 删除 ConversationStore 中不再需要的字段
- 删除 extractAnthropicConvID/containsToolResult 等旧函数
- 编译测试

### Task 5: 编译 + 重启验证
