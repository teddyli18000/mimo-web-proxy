package handler

import (
	"fmt"
	"strings"

	"github.com/teddyli18000/mimo-web-proxy/internal/adapter"
)

// buildToolPrompt builds a concise tool description string from OpenAI tool definitions.
// Injected into the query so MiMo knows what tools are available.
//
// 明确给出调用格式：上游网页端没有 tools 参数，模型必须按约定文本格式输出工具调用，
// 网关再解析回 OpenAI tool_calls。不写格式时模型倾向于直接作答（2026-09 实测：
// flash/pro 在"隐含需要工具"的场景会凭记忆回答而不调用工具）。
func buildToolPrompt(tools []adapter.OpenAITool) string {
	var sb strings.Builder
	sb.WriteString("You have the following tools available. ")
	sb.WriteString("Use them whenever the task requires actions like reading/writing files, running commands, asking questions, or web searches. ")
	sb.WriteString("Do not answer from memory when a tool can provide the answer.\n\n")
	for _, tool := range tools {
		name := tool.Function.Name
		desc := tool.Function.Description
		sb.WriteString(fmt.Sprintf("- %s: %s\n", name, desc))
		if tool.Function.Parameters != nil {
			if params, ok := tool.Function.Parameters.(map[string]interface{}); ok {
				if props, ok := params["properties"].(map[string]interface{}); ok && len(props) > 0 {
					sb.WriteString("  Parameters: ")
					first := true
					for pname := range props {
						if !first {
							sb.WriteString(", ")
						}
						sb.WriteString(pname)
						first = false
					}
					sb.WriteString("\n")
				}
			}
		}
	}
	sb.WriteString("\nTo call one or more tools, output exactly this format:\n")
	sb.WriteString("<tool_calls>\n")
	sb.WriteString("<invoke name=\"TOOL_NAME\">\n")
	sb.WriteString("<parameter name=\"PARAM_NAME\">VALUE</parameter>\n")
	sb.WriteString("</invoke>\n")
	sb.WriteString("</tool_calls>\n")
	sb.WriteString("Put several <invoke> blocks inside one <tool_calls> block to call multiple tools at once. ")
	sb.WriteString("If a tool is needed, you MUST emit the <tool_calls> block in the same reply — ")
	sb.WriteString("never say you already used a tool without emitting it. ")
	sb.WriteString("After tool results come back, continue the task or answer the user.\n")
	return sb.String()
}

// buildToolReminder 生成紧凑的工具提醒（仅名称）。
// 延续会话时使用：完整工具 schema 已在首轮上下文里，重复发送会浪费 20K+ 预算，
// 仅提醒可用工具名即可让模型保持工具意识。
func buildToolReminder(tools []adapter.OpenAITool) string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		if n := strings.TrimSpace(tool.Function.Name); n != "" {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return "Available tools (schemas already provided earlier in this conversation): " + strings.Join(names, ", ")
}
