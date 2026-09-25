package handler

import (
	"fmt"
	"sort"
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
	// 措辞经过对照实验：模型（尤其 pro）对天气/价格这类"自己也能答"的问题倾向
	// 直接作答而不调工具。"MUST call … your own knowledge may be stale" 这组措辞
	// 把调用率从 3/4 提到 4/4；反过来谎称"你没有联网能力"会降到 2/4。
	sb.WriteString("You have the following tools available. ")
	sb.WriteString("You MUST call the matching tool whenever one applies — including questions about current facts such as weather, time, prices, or news. ")
	sb.WriteString("Your own knowledge may be stale; the tool returns live data. Do not answer such questions directly.\n\n")
	for _, tool := range tools {
		name := tool.Function.Name
		// 描述压成单行：工具描述常含多行 Markdown 列表，换行会让 "- " 条目在模型
		// 眼里变成同级的新工具，后面的 Parameters 行也会被吸附到描述末尾
		desc := strings.Join(strings.Fields(tool.Function.Description), " ")
		sb.WriteString(fmt.Sprintf("- %s: %s\n", name, desc))
		if tool.Function.Parameters != nil {
			if params, ok := tool.Function.Parameters.(map[string]interface{}); ok {
				props, _ := params["properties"].(map[string]interface{})
				required, _ := params["required"].([]interface{})
				reqSet := make(map[string]bool, len(required))
				for _, r := range required {
					if s, ok := r.(string); ok {
						reqSet[s] = true
					}
				}
				if len(props) > 0 {
					// 参数必须带类型与必填标记：只列名字时模型经常漏必填项
					// （2026-09 实测：DSH 会话反复出现 missing required property "description"/"pattern"）
					names := make([]string, 0, len(props))
					for pname := range props {
						names = append(names, pname)
					}
					sort.Strings(names)
					specs := make([]string, 0, len(names))
					for _, pname := range names {
						pType := "any"
						if pm, ok := props[pname].(map[string]interface{}); ok {
							if t, ok := pm["type"].(string); ok && t != "" {
								pType = t
							}
						}
						mark := ""
						if reqSet[pname] {
							mark = " (required)"
						}
						specs = append(specs, fmt.Sprintf("%s: %s%s", pname, pType, mark))
					}
					sb.WriteString("  Parameters: " + strings.Join(specs, ", ") + "\n")
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
	sb.WriteString("If a tool applies, you MUST emit the <tool_calls> block in the same reply — ")
	sb.WriteString("answering directly instead of calling the tool is an error. ")
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
