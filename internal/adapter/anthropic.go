package adapter

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// AnthropicRequest 是 Anthropic Messages API 格式
type AnthropicRequest struct {
	Model    string             `json:"model"`
	Messages []AnthropicMessage `json:"messages"`
	// System 允许字符串或 content block 数组：官方规范两种都合法，Claude Code
	// 等客户端发的是数组（带 cache_control 标记），声明成 string 会直接反序列化
	// 失败并返回 400。取文本时走 prompt.NormalizeContent 统一处理。
	System      interface{}     `json:"system,omitempty"`
	MaxTokens   int             `json:"max_tokens"`
	Stream      bool            `json:"stream"`
	Temperature *float64        `json:"temperature,omitempty"`
	Tools       []AnthropicTool `json:"tools,omitempty"`
}

// AnthropicTool 是 Anthropic 格式的工具定义
type AnthropicTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// AnthropicToolUseBlock 是 Anthropic 格式的工具调用块
type AnthropicToolUseBlock struct {
	Type  string                 `json:"type"` // "tool_use"
	ID    string                 `json:"id"`
	Name  string                 `json:"name"`
	Input map[string]interface{} `json:"input"`
}

// AnthropicMessage 是 Anthropic 格式的消息
type AnthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

// AnthropicResponse 是 Anthropic 格式的响应
type AnthropicResponse struct {
	ID           string               `json:"id"`
	Type         string               `json:"type"`
	Role         string               `json:"role"`
	Content      []AnthropicBlock     `json:"content"`
	Model        string               `json:"model"`
	StopReason   string               `json:"stop_reason"`
	Usage        AnthropicUsage       `json:"usage"`
}

// AnthropicBlock 是内容块
type AnthropicBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// AnthropicUsage 是 token 使用量
type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// AnthropicStreamEvent 是流式事件
type AnthropicStreamEvent struct {
	Type  string      `json:"type"`
	Index int         `json:"index,omitempty"`
	Delta interface{} `json:"delta,omitempty"`
}

// AnthropicTextDelta 是文本增量
type AnthropicTextDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}


func MakeAnthropicResponse(model, content string) []byte {
	resp := AnthropicResponse{
		ID:      fmt.Sprintf("msg_%s", uuid.New().String()[:24]),
		Type:    "message",
		Role:    "assistant",
		Content: []AnthropicBlock{{Type: "text", Text: content}},
		Model:   model,
		StopReason: "end_turn",
	}
	data, _ := json.Marshal(resp)
	return data
}

// MakeAnthropicResponseWithUsage 生成带 usage 的非流式响应
func MakeAnthropicResponseWithUsage(model, content string, inTokens, outTokens int) []byte {
	resp := AnthropicResponse{
		ID:         fmt.Sprintf("msg_%s", uuid.New().String()[:24]),
		Type:       "message",
		Role:       "assistant",
		Content:    []AnthropicBlock{{Type: "text", Text: content}},
		Model:      model,
		StopReason: "end_turn",
		Usage:      AnthropicUsage{InputTokens: inTokens, OutputTokens: outTokens},
	}
	data, _ := json.Marshal(resp)
	return data
}

// MakeAnthropicStreamEvent 序列化 Anthropic 流式事件。
//
// Anthropic SSE 的 data 字段就是事件对象本身，不能再包一层信封：
//
//	event: content_block_delta
//	data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}
//
// 此前实现包了 {"type":..,"created":..,"data":{..}}，导致官方 SDK 读不到
// delta（流式有事件但无文本），2026-09 用 @anthropic-ai/sdk 实测定位。
func MakeAnthropicStreamEvent(eventType string, data interface{}) []byte {
	if data == nil {
		b, _ := json.Marshal(map[string]interface{}{"type": eventType})
		return b
	}
	b, _ := json.Marshal(data)
	return b
}

// ConvertAnthropicToolsToOpenAI converts Anthropic tool definitions to OpenAI format
func ConvertAnthropicToolsToOpenAI(tools []AnthropicTool) []OpenAITool {
	if len(tools) == 0 {
		return nil
	}
	result := make([]OpenAITool, 0, len(tools))
	for _, t := range tools {
		result = append(result, OpenAITool{
			Type: "function",
			Function: OpenAIToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}
	return result
}
