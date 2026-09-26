package handler

import (
	"encoding/json"
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
	if m.Role == "assistant" {
		return renderAnthropicAssistant(m)
	}
	return prompt.NormalizeContent(m.Content)
}

// renderAnthropicAssistant 渲染 Anthropic 的 assistant 消息。
// prompt.NormalizeContent 会跳过 tool_use 块，直接用它会让历史里的工具调用
// 退化成空的 "assistant: "，模型看不到自己上一轮调用了什么（多轮工具循环会断）。
func renderAnthropicAssistant(m adapter.AnthropicMessage) string {
	blocks, ok := m.Content.([]interface{})
	if !ok {
		return "assistant: " + strings.TrimSpace(prompt.NormalizeContent(m.Content))
	}
	var text string
	var calls []string
	for _, b := range blocks {
		bm, ok := b.(map[string]interface{})
		if !ok {
			continue
		}
		switch bm["type"] {
		case "text":
			if t, ok := bm["text"].(string); ok {
				text += t
			}
		case "tool_use":
			name, _ := bm["name"].(string)
			args, _ := json.Marshal(bm["input"])
			calls = append(calls, fmt.Sprintf("%s(%s)", name, string(args)))
		}
	}
	var b strings.Builder
	b.WriteString("assistant:")
	if len(calls) > 0 {
		b.WriteString(" [Tool Calls]\n")
		for _, c := range calls {
			b.WriteString(c + "\n")
		}
		if t := strings.TrimSpace(text); t != "" {
			b.WriteString("\n" + t)
		}
		return strings.TrimSuffix(b.String(), "\n")
	}
	b.WriteString(" " + strings.TrimSpace(text))
	return strings.TrimSpace(b.String())
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

// currentTurnQuestion 返回本轮的用户提问。
//
// 本轮 = 最后一个 assistant 消息之后的部分。在其中找第一条"像提问"的 user 消息：
// agent 客户端把用户提问放在本轮靠前、注入上下文跟在后面，但工具结果轮可能在其后
// 追加新指令（[tool, tool, user("改一下")]），注入也可能排在提问之前，因此必须遍历
// 而不是只看第一条。全部命中注入特征时返回空串（说明本轮没有新提问）。
func currentTurnQuestion(msgs []adapter.OpenAIMessage) string {
	lastAssistant := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			lastAssistant = i
			break
		}
	}
	for _, m := range msgs[lastAssistant+1:] {
		if m.Role != "user" {
			continue
		}
		if q := strings.TrimSpace(openAIMessageText(m)); q != "" && !looksLikeInjectedContext(q) {
			return q
		}
	}
	return ""
}

// looksLikeInjectedContext 粗判一段文本是否为客户端注入的上下文而非用户提问。
// 覆盖 DSH 的 <system-reminder> / runtime context 形态；命中时宁可不记锚点，
// 也好过把注入内容当成任务。（TrimSpace 只返回切片，不复制整段文本）
func looksLikeInjectedContext(s string) bool {
	head := strings.TrimSpace(s)
	return strings.HasPrefix(head, "<system-reminder>") ||
		strings.HasPrefix(head, "Current runtime context") ||
		strings.HasPrefix(head, "Current DSH")
}

// isBareConfirmation 判断用户是否只是在应声（"继续"/"好的"/"ok"）。
// 这类回复会覆盖任务锚点，让后续工具轮注入 "Current task: 继续" 而丢掉真任务。
func isBareConfirmation(q string) bool {
	t := strings.ToLower(strings.TrimSpace(q))
	t = strings.TrimRight(t, "。！!.~～ ")
	switch t {
	case "继续", "接着", "往下", "好", "好的", "好嘞", "行", "可以", "嗯", "对", "是",
		"ok", "okay", "yes", "y", "go", "go on", "continue", "next", "sure":
		return true
	}
	return false
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

// currentTurnQuestionAnthropic Anthropic 版本轮提问。
// Anthropic 把工具结果也放在 user 消息里，必须跳过纯 tool_result 的消息，
// 否则工具输出会被当成用户提问记录下来。
func currentTurnQuestionAnthropic(msgs []adapter.AnthropicMessage) string {
	lastAssistant := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			lastAssistant = i
			break
		}
	}
	for _, m := range msgs[lastAssistant+1:] {
		if m.Role != "user" || isAnthropicToolResult(m) {
			continue
		}
		if q := strings.TrimSpace(anthropicUserText(m)); q != "" && !looksLikeInjectedContext(q) {
			return q
		}
	}
	return ""
}

// anthropicUserText 提取 user 消息中的文本块。
// 混合消息（tool_result + text）里若整段交给 NormalizeContent，会把工具输出
// 一起拼进来，导致任务锚点被命令输出污染。
func anthropicUserText(m adapter.AnthropicMessage) string {
	blocks, ok := m.Content.([]interface{})
	if !ok {
		return prompt.NormalizeContent(m.Content)
	}
	var parts []string
	for _, b := range blocks {
		bm, ok := b.(map[string]interface{})
		if !ok || bm["type"] != "text" {
			continue
		}
		if t, ok := bm["text"].(string); ok && t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, "\n")
}

// isAnthropicToolResult 判断该 user 消息是否只是工具结果回传（不含用户文本）
func isAnthropicToolResult(m adapter.AnthropicMessage) bool {
	blocks, ok := m.Content.([]interface{})
	if !ok || len(blocks) == 0 {
		return false
	}
	for _, b := range blocks {
		bm, ok := b.(map[string]interface{})
		if !ok || bm["type"] != "tool_result" {
			return false
		}
	}
	return true
}

// serializeMessagesAnthropic Anthropic 版
func serializeMessagesAnthropic(msgs []adapter.AnthropicMessage, system string, maxChars int, extraSystem ...string) string {
	return serializeRoleTexts(toRoleTextsAnthropic(msgs, system), maxChars, extraSystem...)}

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

	// 分段：历史 / 本轮上下文 / 本轮提问
	//
	// agent 客户端（DSH 等）本轮的消息顺序是【用户提问在前、注入上下文在后】：
	//   [user: 你好] [user: <system-reminder>AGENTS.md…] [user: Current runtime context…] [user: 技能目录]
	// 若把最后一条当 Current Query，模型会把技能目录当成任务——2026-09 实测：
	// 用户只说"你好"，模型却去写一个 quiz runner 脚本（12K 字符的工具调用）。
	// 正确口径：本轮 = 最后一个 assistant 之后的消息；本轮第一条 user 消息才是提问，
	// 其余（注入上下文）作为 Context 放在提问之前。
	lastAssistant := -1
	for i := len(rest) - 1; i >= 0; i-- {
		if rest[i][0] == "assistant" {
			lastAssistant = i
			break
		}
	}
	history := rest[:lastAssistant+1]
	turn := rest[lastAssistant+1:]

	var queryMsg *roleText
	var ctxMsgs []roleText
	if len(turn) > 0 && (turn[0][0] == "tool" || turn[0][0] == "function") {
		// 工具结果轮：工具输出是本轮主体，后续消息作为附加上下文
		queryMsg = &turn[0]
		ctxMsgs = turn[1:]
	} else {
		for i := range turn {
			if turn[i][0] == "user" && strings.TrimSpace(turn[i][1]) != "" {
				queryMsg = &turn[i]
				ctxMsgs = append(append([]roleText{}, turn[:i]...), turn[i+1:]...)
				break
			}
		}
		if queryMsg == nil && len(turn) > 0 {
			// 本轮没有 user 消息（异常形态）：退化为取最后一条
			queryMsg = &turn[len(turn)-1]
			ctxMsgs = turn[:len(turn)-1]
		}
		if queryMsg == nil && len(rest) > 0 {
			// 本轮为空（客户端只发了历史、没带新消息）：退化为把最后一条当提问
			queryMsg = &rest[len(rest)-1]
			history = rest[:len(rest)-1]
			ctxMsgs = nil
		}
	}

	var histStr, ctxStr, queryStr string
	if len(history) > 0 {
		hs := make([]string, 0, len(history))
		for _, m := range history {
			hs = append(hs, formatRoleText(m))
		}
		histStr = "[Conversation History]\n" + strings.Join(hs, "\n\n")
	}
	if len(ctxMsgs) > 0 {
		cs := make([]string, 0, len(ctxMsgs))
		for _, m := range ctxMsgs {
			cs = append(cs, formatRoleText(m))
		}
		ctxStr = "[Context]\n" + strings.Join(cs, "\n\n")
	}
	if queryMsg != nil {
		queryStr = "[Current Query]\n" + formatRoleText(*queryMsg)
	}
	// 没有可发送的本轮内容（例如只给了 system）就返回空串，让调用方按
	// "no valid user message" 拒绝——否则会把一段没有提问的 prompt 发给上游。
	if queryStr == "" {
		return ""
	}

	// 组装：system → 历史 → 上下文 → 提问（提问放最后，模型对末尾注意力最强）
	parts := make([]string, 0, 4)
	if sysStr != "" {
		parts = append(parts, sysStr)
	}
	if histStr != "" {
		parts = append(parts, histStr)
	}
	if ctxStr != "" {
		parts = append(parts, ctxStr)
	}
	if queryStr != "" {
		parts = append(parts, queryStr)
	}
	body := strings.Join(parts, "\n\n")

	// 超限：丢历史与上下文，优先保 Current Query 与 system。
	// system 自身超预算（工具 schema 过大）时必须截断它，否则 body 会突破上游
	// 硬上限被直接拒绝——先保提问，再保 system，最后兜底硬截断。
	if utf8.RuneCountInString(body) > maxChars {
		const notice = "\n\n[Conversation History]\n...(truncated, history too long)\n\n"
		room := maxChars - utf8.RuneCountInString(queryStr) - utf8.RuneCountInString(notice)
		if room < 0 {
			room = 0
		}
		body = truncateHead(sysStr, room) + notice + queryStr
		if utf8.RuneCountInString(body) > maxChars {
			body = truncateTail(body, maxChars)
		}
	}

	return strings.TrimSpace(body)
}

// truncateHead 保留前 n 个字符（按 rune 切，避免截断多字节字符）
func truncateHead(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// truncateTail 保留后 n 个字符
func truncateTail(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

// formatRoleText 渲染单条消息为历史行
func formatRoleText(m roleText) string {
	return m[0] + ": " + m[1]
}

// 确认 fmt 使用（避免未使用 import）
var _ = fmt.Sprintf
