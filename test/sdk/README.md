# SDK 集成测试

用**官方 SDK** 对真实上游跑通，验证网关对各类客户端的兼容性与稳定性。
不 mock 上游——这些测试会真的消耗 MiMo 额度。

## 为什么用官方 SDK

DSH 内部通过 `@earendil-works/pi-ai` 调用 `openai` 官方包解析 SSE；Claude Code 等用
`@anthropic-ai/sdk`。用同一批库做测试，等价于在真实客户端协议栈上验证——比手写
HTTP 断言更能发现"事件发了但 SDK 读不到"这类协议层问题（v1.3.0 修掉的 Anthropic
流式信封 bug 就是这样发现的）。

## 准备

```bash
cd test/sdk
npm install
```

网关需在运行（默认 `http://localhost:8080`），可用 `GW` 环境变量改：

```bash
GW=http://localhost:8090 npm run stability
```

## 套件

| 命令 | 覆盖 | 参考耗时 |
|---|---|---|
| `npm run stability` | 三模型 × 多轮上下文 / 多轮工具调用 / 长上下文 / 流式多轮 | ~5 min |
| `npm run anthropic` | Anthropic 格式：基础 / 多轮 / tool_use / 工具结果回传 / 流式 | ~4 min |
| `npm run long-session` | 12 轮长会话（含工具调用），验证末尾仍记得首轮内容 | ~8 min |
| `npm run long-output` | 3000+ 字长输出完整性（流式与非流式，结尾不截断） | ~6 min |
| `npm run tools` | 工具调用可靠性（明确/隐含/命令三类场景） | ~2 min |

## 期望结果

全部 `0 FAIL`。`stability` 连续跑多遍也应全绿——会话匹配的偶发串会话问题
（v1.3.0 修复）就是靠重复运行暴露的。

## 说明

- 这些脚本直接打网关，需要有效 Cookie 与可用额度
- 上游限流时（fastchat 通道较严）可能出现个别 429，网关会自动退避重试
- 测试会创建真实会话记录，介意的话用专门的小号
