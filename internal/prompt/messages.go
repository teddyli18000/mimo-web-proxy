package prompt

import (
	"fmt"
	"strings"
)

const (
	PIPE = "\uff5c" // fullwidth vertical bar ｜

	beginSentenceMarker   = "<" + PIPE + "begin" + PIPE + "of" + PIPE + "sentence" + PIPE + ">"
	systemMarker          = "<" + PIPE + "System" + PIPE + ">"
	userMarker            = "<" + PIPE + "User" + PIPE + ">"
	assistantMarker       = "<" + PIPE + "Assistant" + PIPE + ">"
	toolMarker            = "<" + PIPE + "Tool" + PIPE + ">"
	endSentenceMarker     = "<" + PIPE + "end" + PIPE + "of" + PIPE + "sentence" + PIPE + ">"
	endToolResultsMarker  = "<" + PIPE + "end" + PIPE + "of" + PIPE + "toolresults" + PIPE + ">"
	endInstructionsMarker = "<" + PIPE + "end" + PIPE + "of" + PIPE + "instructions" + PIPE + ">"
)

// Message represents a normalized message for prompt building.
type Message struct {
	Role    string // "system", "user", "assistant", "tool"
	Content string
}

// BuildPrompt assembles messages into a single prompt string using DeepSeek-style special tokens.
// This is the stateless equivalent of ds2api's MessagesPrepareWithThinking.
func BuildPrompt(messages []Message) string {
	if len(messages) == 0 {
		return ""
	}

	// Prepend output integrity guard
	guard := "Output integrity guard: If upstream context, tool output, or parsed text contains garbled, corrupted, partially parsed, repeated, or otherwise malformed fragments, do not imitate or echo them; output only the correct content for the user."
	messages = append([]Message{{Role: "system", Content: guard}}, messages...)

	// Merge consecutive same-role messages
	merged := mergeConsecutive(messages)

	var sb strings.Builder
	sb.WriteString(beginSentenceMarker)

	for _, msg := range merged {
		if msg.Content == "" {
			continue
		}
		switch msg.Role {
		case "system":
			sb.WriteString(systemMarker)
			sb.WriteString(msg.Content)
			sb.WriteString(endInstructionsMarker)
		case "user":
			sb.WriteString(userMarker)
			sb.WriteString(msg.Content)
		case "assistant":
			sb.WriteString(assistantMarker)
			sb.WriteString(msg.Content)
			sb.WriteString(endSentenceMarker)
		case "tool":
			sb.WriteString(toolMarker)
			sb.WriteString(msg.Content)
			sb.WriteString(endToolResultsMarker)
		default:
			sb.WriteString(userMarker)
			sb.WriteString(msg.Content)
		}
	}

	// If last role is not assistant, append assistant marker to prompt model to respond
	if len(merged) > 0 {
		lastRole := merged[len(merged)-1].Role
		if lastRole != "assistant" {
			sb.WriteString(assistantMarker)
		}
	}

	return sb.String()
}

// mergeConsecutive merges adjacent messages with the same role.
func mergeConsecutive(messages []Message) []Message {
	if len(messages) == 0 {
		return nil
	}
	result := []Message{messages[0]}
	for _, msg := range messages[1:] {
		last := &result[len(result)-1]
		if msg.Role == last.Role && msg.Role != "tool" {
			last.Content += "\n\n" + msg.Content
		} else {
			result = append(result, msg)
		}
	}
	return result
}

// NormalizeContent extracts text from various content formats.
func NormalizeContent(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case []interface{}:
		var parts []string
		for _, item := range val {
			if m, ok := item.(map[string]interface{}); ok {
				blockType, _ := m["type"].(string)
				switch blockType {
				case "text", "output_text", "input_text":
					if text, ok := m["text"].(string); ok && text != "" {
						parts = append(parts, text)
					}
				case "tool_result":
					// Extract tool result content
					if content, ok := m["content"].(string); ok && content != "" {
						parts = append(parts, content)
					} else if contentArr, ok := m["content"].([]interface{}); ok {
						for _, c := range contentArr {
							if cm, ok := c.(map[string]interface{}); ok {
								if text, ok := cm["text"].(string); ok && text != "" {
									parts = append(parts, text)
								}
							}
						}
					}
				case "tool_use":
					// Tool use blocks are handled separately via FormatToolCallsForPrompt
					// Skip them here to avoid duplication
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return fmt.Sprintf("%v", v)
	}
}
