package promptcompat

import (
	"strings"

	"github.com/teddyli18000/mimo-web-proxy/internal/adapter"
	"github.com/teddyli18000/mimo-web-proxy/internal/prompt"
)

// NormalizeOpenAIMessages converts OpenAI messages to normalized prompt messages.
// This is the stateless equivalent of ds2api's NormalizeOpenAIMessagesForPrompt.
func NormalizeOpenAIMessages(msgs []adapter.OpenAIMessage) []prompt.Message {
	result := make([]prompt.Message, 0, len(msgs))
	for _, msg := range msgs {
		role := msg.Role
		content := ""

		switch role {
		case "system", "developer":
			if role == "developer" {
				role = "system"
			}
			content = prompt.NormalizeContent(msg.Content)

		case "user":
			content = prompt.NormalizeContent(msg.Content)

		case "assistant":
			content = buildAssistantContent(msg)

		case "tool", "function":
			content = buildToolResultContent(msg)

		default:
			content = prompt.NormalizeContent(msg.Content)
			if role == "" {
				role = "user"
			}
		}

		if content == "" {
			continue
		}
		result = append(result, prompt.Message{Role: role, Content: content})
	}
	return result
}

// NormalizeAnthropicMessages converts Anthropic messages to normalized prompt messages.
func NormalizeAnthropicMessages(msgs []adapter.AnthropicMessage, system string) []prompt.Message {
	result := make([]prompt.Message, 0, len(msgs)+1)

	// Add system message first
	if system != "" {
		result = append(result, prompt.Message{Role: "system", Content: system})
	}

	for _, msg := range msgs {
		role := msg.Role
		content := ""

		switch role {
		case "user":
			content = buildAnthropicUserContent(msg)

		case "assistant":
			content = buildAnthropicAssistantContent(msg)

		default:
			content = prompt.NormalizeContent(msg.Content)
		}

		if content == "" {
			continue
		}
		result = append(result, prompt.Message{Role: role, Content: content})
	}
	return result
}

// buildAssistantContent builds assistant message content with tool_calls history.
func buildAssistantContent(msg adapter.OpenAIMessage) string {
	var parts []string

	// 1. Text content
	text := prompt.NormalizeContent(msg.Content)
	if text != "" {
		parts = append(parts, text)
	}

	// 2. Tool calls rendered as DSML XML
	if len(msg.ToolCalls) > 0 {
		tcInterface := make([]interface{}, len(msg.ToolCalls))
		for i, tc := range msg.ToolCalls {
			tcMap := map[string]interface{}{
				"id":   tc.ID,
				"type": tc.Type,
			}
			if tc.Function.Name != "" {
				tcMap["function"] = map[string]interface{}{
					"name":      tc.Function.Name,
					"arguments": tc.Function.Arguments,
				}
			}
			tcInterface[i] = tcMap
		}
		rendered := prompt.FormatToolCallsForPrompt(tcInterface)
		if rendered != "" {
			parts = append(parts, rendered)
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n")
}

// buildToolResultContent builds tool result message content.
func buildToolResultContent(msg adapter.OpenAIMessage) string {
	content := prompt.NormalizeContent(msg.Content)
	if content == "" {
		return "null"
	}
	return content
}

// buildAnthropicUserContent handles Anthropic user messages which may contain tool_result blocks.
func buildAnthropicUserContent(msg adapter.AnthropicMessage) string {
	switch v := msg.Content.(type) {
	case string:
		return v
	case []interface{}:
		var texts []string
		for _, block := range v {
			if m, ok := block.(map[string]interface{}); ok {
				blockType, _ := m["type"].(string)
				switch blockType {
				case "text":
					if text, ok := m["text"].(string); ok && text != "" {
						texts = append(texts, text)
					}
				case "tool_result":
					content := extractToolResultContent(m)
					if content != "" {
						texts = append(texts, content)
					}
				}
			}
		}
		return strings.Join(texts, "\n")
	default:
		return prompt.NormalizeContent(v)
	}
}

// buildAnthropicAssistantContent handles Anthropic assistant messages with tool_use blocks.
func buildAnthropicAssistantContent(msg adapter.AnthropicMessage) string {
	switch v := msg.Content.(type) {
	case string:
		return v
	case []interface{}:
		var texts []string
		var toolCalls []interface{}

		for _, block := range v {
			if m, ok := block.(map[string]interface{}); ok {
				blockType, _ := m["type"].(string)
				switch blockType {
				case "text":
					if text, ok := m["text"].(string); ok && text != "" {
						texts = append(texts, text)
					}
				case "tool_use":
					// Convert Anthropic tool_use to OpenAI-like format for rendering
					tc := map[string]interface{}{
						"id":   m["id"],
						"type": "function",
						"function": map[string]interface{}{
							"name":      m["name"],
							"arguments": m["input"],
						},
					}
					toolCalls = append(toolCalls, tc)
				}
			}
		}

		// Render tool calls as DSML XML
		if len(toolCalls) > 0 {
			rendered := prompt.FormatToolCallsForPrompt(toolCalls)
			if rendered != "" {
				texts = append(texts, rendered)
			}
		}

		return strings.Join(texts, "\n\n")
	default:
		return prompt.NormalizeContent(v)
	}
}

// extractToolResultContent extracts content from a tool_result block.
func extractToolResultContent(m map[string]interface{}) string {
	if content, ok := m["content"].(string); ok && content != "" {
		return content
	}
	if contentArr, ok := m["content"].([]interface{}); ok {
		var parts []string
		for _, c := range contentArr {
			if cm, ok := c.(map[string]interface{}); ok {
				if text, ok := cm["text"].(string); ok && text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// isToolResultMessage checks if an Anthropic message contains tool_result blocks.
func isToolResultMessage(msg adapter.AnthropicMessage) bool {
	if msg.Role != "user" {
		return false
	}
	if arr, ok := msg.Content.([]interface{}); ok {
		for _, block := range arr {
			if m, ok := block.(map[string]interface{}); ok {
				if t, _ := m["type"].(string); t == "tool_result" {
					return true
				}
			}
		}
	}
	return false
}
