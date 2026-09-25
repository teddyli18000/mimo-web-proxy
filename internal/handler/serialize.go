package handler

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/teddyli18000/mimo-web-proxy/internal/adapter"
	"github.com/teddyli18000/mimo-web-proxy/internal/prompt"
)

// =============================================================================
// serializeMessages — 全量重放式 query 组装（参考 meny2333/mimo2api_go 的设计）
//
// 背景：agent 类客户端（DSH/Cline/Claude Code 等）发来的 messages 结构是
//   [system, user(真实提问), user(注入:AGENTS.md), user(注入:runtime), user(注入:skill-catalog), ...]
// wtz44 原实现"只取最后一条 user 消息当 query"会把注入物当问题、丢掉真实提问
// （2026-09 DSH 实测：模型收到 16K 的 skill-catalog 后去 fetch GitHub Copilot 文档）。
//
// 修复：每次请求把完整对话渲染成三段式文本发给 MiMo：
//   [System Instruction]  — 全部 system 消息合并
//   [Conversation History]— 除最后一条外的非系统消息（assistant 含 tool_calls 特殊渲染）
//   [Current Query]       — 最后一条非系统消息（永远是用户的最新输入/工具结果）
// 配合 convstore 的 fingerprint 会话延续判断，客户端自己管理历史、网关不猜。
// =============================================================================

// MaxQueryChars 单次请求的 query 字符上限（超出丢老历史，保 system 与最新消息）


// roleText 一条 (role, textContent)；convstore.Fingerprint 接受 [][2]string
type roleText = [2]string

func mkRT(role, text string) roleText { return [2]string{role, text} }

// toRoleTexts 将 OpenAI 消息列表转为 (role, text) 序列（供 convstore fingerprint 使用）
func toRoleTexts(msgs []adapter.OpenAIMessage) []roleText {
	out := make([]roleText, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, mkRT(m.Role, openAIMessageText(m)))
	}
	return out
}

// toRoleTextsAnthropic 同上，Anthropic 版（system 独立字段并入序列头部）
func toRoleTextsAnthropic(msgs []adapter.AnthropicMessage, system string) []roleText {
	out := make([]roleText, 0, len(msgs)+1)
	if system != "" {
		out = append(out, mkRT("system", system))
	}
	for _, m := range msgs {
		out = append(out, mkRT(m.Role, anthropicMessageText(m)))
	}
	return out
}

// openAIMessageText 提取 OpenAI 消息的纯文本（含 tool_calls / tool 结果的文本化）
func openAIMessageText(m adapter.OpenAIMessage) string {
	switch m.Role {
	case "assistant":
		return renderAssistant(m)
	case "tool":
		return renderToolResult(m.ToolCallID, m.Content)
	default:
		return prompt.NormalizeContent(m.Content)
	}
}

// anthropicMessageText 提取 Anthropic 消息的纯文本
func anthropicMessageText(m adapter.AnthropicMessage) string {
	return prompt.NormalizeContent(m.Content)
}

// renderAssistant 渲染 assistant 消息（带 tool_calls 时显式标注调用与参数）
func renderAssistant(m adapter.OpenAIMessage) string {
	var b strings.Builder
	b.WriteString("assistant:")
	if len(m.ToolCalls) > 0 {
		b.WriteString(" [Tool Calls]\n")
		for _, tc := range m.ToolCalls {
			b.WriteString(tc.Function.Name + "(" + tc.Function.Arguments + ")\n")
		}
		if s := prompt.NormalizeContent(m.Content); s != "" {
			b.WriteString("\n" + s)
		}
		return strings.TrimSuffix(b.String(), "\n")
	}
	b.WriteString(" " + prompt.NormalizeContent(m.Content))
	return strings.TrimSpace(b.String())
}

// renderToolResult 渲染 tool 结果消息
func renderToolResult(toolCallID string, content interface{}) string {
	ref := ""
	if toolCallID != "" {
		ref = " (" + toolCallID + ")"
	}
	return "[Tool Result]" + ref + ":\n" + prompt.NormalizeContent(content)
}

// serializeMessages 全量重放组装 query（extraSystem: 额外注入 system 的文本如工具定义，纳入长度预算）
func serializeMessages(msgs []adapter.OpenAIMessage, maxChars int, extraSystem ...string) string {
	return serializeRoleTexts(toRoleTexts(msgs), maxChars, extraSystem...)
}

// DeltaMessages 返回本轮新增的消息（最后一个 assistant 消息之后的部分）。
//
// agent 客户端（DSH/Cline 等）每轮都会重发完整历史，而上游 MiMo 服务端已保有
// 此前对话上下文（conversationId + parentId 链）。延续会话时只发送增量：
//   - 大幅缩短 query，避免触发上游长度限制（fastchat ≈48K / open-apis ≈100K）
//   - 与网页端原生行为一致（网页端每轮也只发新消息）
// 无 assistant 消息（首轮）或增量为空时返回原列表，退化为全量发送。
func DeltaMessages(msgs []adapter.OpenAIMessage) []adapter.OpenAIMessage {
	lastAssistant := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			lastAssistant = i
			break
		}
	}
	if lastAssistant < 0 || lastAssistant+1 >= len(msgs) {
		return msgs
	}
	tail := msgs[lastAssistant+1:]
	for _, m := range tail {
		if m.Role == "user" || m.Role == "tool" || m.Role == "function" {
			return tail
		}
	}
	return msgs
}

// DeltaMessagesAnthropic Anthropic 版增量
func DeltaMessagesAnthropic(msgs []adapter.AnthropicMessage) []adapter.AnthropicMessage {
	lastAssistant := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			lastAssistant = i
			break
		}
	}
	if lastAssistant < 0 || lastAssistant+1 >= len(msgs) {
		return msgs
	}
	tail := msgs[lastAssistant+1:]
	for _, m := range tail {
		if m.Role == "user" {
			return tail
		}
	}
	return msgs
}

// serializeMessagesAnthropic Anthropic 版
func serializeMessagesAnthropic(msgs []adapter.AnthropicMessage, system string, maxChars int, extraSystem ...string) string {
	return serializeRoleTexts(toRoleTextsAnthropic(msgs, system), maxChars, extraSystem...)
}

// serializeRoleTexts 三段式渲染 + 长度截断（extraSystem 并入 System 段并计入预算）
func serializeRoleTexts(rts []roleText, maxChars int, extraSystem ...string) string {
	var system []string
	var rest []roleText
	for _, m := range rts {
		if m[0] == "system" {
			if t := strings.TrimSpace(m[1]); t != "" {
				system = append(system, t)
			}
		} else {
			if m[1] == "" && m[0] != "assistant" {
				continue // 空 user/tool 消息跳过
			}
			rest = append(rest, m)
		}
	}
	if len(rest) == 0 && len(system) == 0 {
		return ""
	}

	if len(extraSystem) > 0 {
		system = append(append([]string{}, system...), extraSystem...)
	}
	var sysStr string
	if len(system) > 0 {
		sysStr = "[System Instruction]\n" + strings.Join(system, "\n\n")
	}

	// 历史段：rest 去掉最后一条（当前消息）
	var histStr, queryStr string
	if len(rest) > 0 {
		queryStr = "[Current Query]\n" + formatRoleText(rest[len(rest)-1])
		if len(rest) > 1 {
			var hs []string
			for _, m := range rest[:len(rest)-1] {
				hs = append(hs, formatRoleText(m))
			}
			histStr = "[Conversation History]\n" + strings.Join(hs, "\n\n")
		}
	}

	// 组装 + 长度预算截断（优先保 system 与 Current Query，丢老历史）
	var body string
	if histStr != "" {
		body = histStr + "\n\n" + queryStr
	} else {
		body = queryStr
	}
	if sysStr != "" {
		body = sysStr + "\n\n" + body
	}

	// 超限：从历史段头部截（保留 Current Query 完整）
	if utf8.RuneCountInString(body) > maxChars {
		sysPart := ""
		if sysStr != "" {
			sysPart = sysStr + "\n\n"
		}
		budget := maxChars - utf8.RuneCountInString(sysPart) - 40
		if budget < 2000 {
			budget = 2000
		}
		q := "[Conversation History]\n...(truncated, history too long)\n\n" + queryStr
		if utf8.RuneCountInString(q) > budget {
			rq := []rune(queryStr)
			if len(rq) > budget {
				q = string(rq[len(rq)-budget:])
			}
			body = sysPart + q
		} else {
			body = sysPart + q
		}
	}

	return strings.TrimSpace(body)
}

// formatRoleText 渲染单条消息为历史行
func formatRoleText(m roleText) string {
	return m[0] + ": " + m[1]
}

// 确认 fmt 使用（避免未使用 import）
var _ = fmt.Sprintf
