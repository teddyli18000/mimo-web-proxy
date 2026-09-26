# SDK 集成测试

用**官方 SDK** 对真实上游跑通，验证网关对各类客户端的兼容性与稳定性。
不 mock 上游——这些测试会真的消耗 MiMo 额度。

## 为什么用官方 SDK

DSH 内部通过 `@earendil-works/pi-ai` 调用 `openai` 官方包解析 SSE；Claude Code 等用
`@anthropic-ai/sdk`。用同一批库做测试，等价于在真实客户端协议栈上验证——比手写
HTTP 断言更能发现"事件发了但 SDK 读不到"这类协议层问题（Anthropic 流式信封 bug、
流式工具调用缺 `index`、usage 块覆盖 `finish_reason` 都是这样发现的）。

## 准备

```bash
cd test/sdk
npm install
```

网关需在运行（默认 `http://localhost:8080`），可用 `GW` 环境变量改：

```bash
GW=http://localhost:8090 npm run stability
```

`config` 与 `images` 两套会**改写 `default_model` 配置**，建议指向测试实例。

## 套件

| 命令 | 覆盖 |
|---|---|
| `npm run all` | 依次跑下面 7 套（约 25 分钟） |
| `npm run edges` | 请求校验与错误码、Unicode 保真、长输入截断、流式事件顺序、多工具调用、会话隔离 |
| `npm run concurrency` | 8 路并发、并发下的会话隔离、10 轮串行上下文、流式与非流式混合负载 |
| `npm run protocol` | 流式协议合规：同一流 id 一致、usage 块 `choices` 为空、`finish_reason` 不被覆盖、工具调用带 `index` |
| `npm run images` | 图片端到端：三模型识图、缓存键正确、Anthropic 图片块、带图 + 工具、纯文本不受影响 |
| `npm run stability` | 三模型 × 多轮上下文 / 多轮工具调用 / 长上下文 / 流式多轮 |
| `npm run anthropic` | Anthropic 格式：基础 / 多轮 / tool_use / 工具结果回传 / 流式 |
| `npm run config` | `default_model` 是否生效、Anthropic `system` 数组、多轮工具历史、管理接口 |
| `npm run anthropic-tools` | Anthropic 流式工具调用的事件序列与 SDK 解析 |
| `npm run tools` | 工具调用可靠性（明确要求 / 隐含需要 / 命令三类场景） |
| `npm run tool-precision` | 双向：该调工具时调、不该调时保持安静（防过度调用） |
| `npm run long-session` | 12 轮长会话（含工具调用），验证末尾仍记得首轮内容 |
| `npm run long-output` | 3000+ 字长输出完整性（流式与非流式，结尾不截断） |

## 复现脚本（排查用，非日常回归）

| 脚本 | 用途 |
|---|---|
| `repro-injected-context.mjs` | 回放"用户只说你好、模型却去写脚本"的原始请求，验证当前提问识别正确 |
| `repro-empty.mjs` | 空响应时打印原始 HTTP 响应体，用于定位上游返回了什么 |

## 期望结果

全部 `0 FAIL`。稳定性套件连跑多遍也应全绿——会话匹配的偶发串会话问题就是靠
重复运行暴露的。

## 说明

- 天气类问题不适合用来测工具调用：模型自带实时天气数据，会直接作答而不调工具
  （实测 pro 在 temperature 0.8/0.3/0.1 下都出现过）。测的是模型偏好而非网关，
  所以工具用例都改用"读文件/执行命令"这类模型无法凭自身知识回答的任务
- 上游偶发限流（fastchat 通道较严）或瞬时停顿，网关会自动退避重试；单次失败可
  先重跑确认
- 测试会创建真实会话记录，介意的话用专门的小号
