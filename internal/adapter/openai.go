package adapter

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// OpenAIChatRequest 是 OpenAI 格式的请求
type OpenAIChatRequest struct {
	Model       string          `json:"model"`
	Messages    []OpenAIMessage `json:"messages"`
	Stream      bool            `json:"stream"`
	Temperature *float64        `json:"temperature,omitempty"`
	MaxTokens   *int            `json:"max_tokens,omitempty"`
	Tools       []OpenAITool    `json:"tools,omitempty"`
	ToolChoice  interface{}     `json:"tool_choice,omitempty"`
}

// OpenAITool 是 OpenAI 格式的工具定义
type OpenAITool struct {
	Type     string             `json:"type"`
	Function OpenAIToolFunction `json:"function"`
}

// OpenAIToolFunction 是工具函数定义
type OpenAIToolFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  interface{} `json:"parameters,omitempty"`
}

// OpenAIMessage 是 OpenAI 格式的消息
// OpenAIMessage 是 OpenAI 格式的消息
type OpenAIMessage struct {
	Role       string           `json:"role"`
	Content    interface{}      `json:"content"` // string 或 []ContentPart
	ToolCalls  []OpenAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

// OpenAIToolCall 是 OpenAI 格式的工具调用
type OpenAIToolCall struct {
	// Index 仅流式响应使用：客户端按它把分片的工具调用拼回完整调用
	Index    *int               `json:"index,omitempty"`
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function OpenAIToolCallFunc `json:"function"`
}

// OpenAIToolCallFunc 是工具调用函数
type OpenAIToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// OpenAIChatResponse 是 OpenAI 格式的响应
type OpenAIChatResponse struct {
	ID      string           `json:"id"`
	Object  string           `json:"object"`
	Created int64            `json:"created"`
	Model   string           `json:"model"`
	Choices []OpenAIChoice   `json:"choices"`
	Usage   *OpenAIUsage     `json:"usage,omitempty"`
}

// OpenAIChoice 是 OpenAI 格式的选项
type OpenAIChoice struct {
	Index        int            `json:"index"`
	Message      *OpenAIMessage `json:"message,omitempty"`
	Delta        *OpenAIDelta   `json:"delta,omitempty"`
	FinishReason *string        `json:"finish_reason,omitempty"`
}

// OpenAIDelta 是流式增量
type OpenAIDelta struct {
	Role      string           `json:"role,omitempty"`
	Content   string           `json:"content,omitempty"`
	ToolCalls []OpenAIToolCall `json:"tool_calls,omitempty"`
}

// OpenAIUsage 是 token 使用量
type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// OpenAIStreamChunk 是流式响应块
type OpenAIStreamChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []OpenAIChoice `json:"choices"`
	Usage   *OpenAIUsage   `json:"usage,omitempty"`
}


// NewStreamID 生成流式响应 id。
// OpenAI 规范要求同一个流的全部 chunk 共用同一个 id（客户端据此归组）。
func NewStreamID() string {
	return fmt.Sprintf("chatcmpl-%s", uuid.New().String()[:8])
}

func marshalChunk(id, model string, choices []OpenAIChoice, usage *OpenAIUsage) []byte {
	data, _ := json.Marshal(OpenAIStreamChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: choices,
		Usage:   usage,
	})
	return data
}

// MakeOpenAIStreamContentChunk 内容增量块
func MakeOpenAIStreamContentChunk(id, model, content string) []byte {
	return marshalChunk(id, model, []OpenAIChoice{
		{Index: 0, Delta: &OpenAIDelta{Content: content}},
	}, nil)
}

// MakeOpenAIStreamFinishChunk 收尾块（finish_reason=stop）
func MakeOpenAIStreamFinishChunk(id, model string) []byte {
	fr := "stop"
	return marshalChunk(id, model, []OpenAIChoice{
		{Index: 0, Delta: &OpenAIDelta{}, FinishReason: &fr},
	}, nil)
}

// MakeOpenAIStreamToolCallChunk 工具调用块（finish_reason=tool_calls）。
// 每个调用带 index：OpenAI 规范要求流式工具调用分片用 index 归组，
// 缺了它客户端无法把分片拼回完整调用。
func MakeOpenAIStreamToolCallChunk(id, model string, toolCalls []OpenAIToolCall) []byte {
	indexed := make([]OpenAIToolCall, len(toolCalls))
	for i, tc := range toolCalls {
		idx := i
		tc.Index = &idx
		indexed[i] = tc
	}
	fr := "tool_calls"
	return marshalChunk(id, model, []OpenAIChoice{
		{Index: 0, Delta: &OpenAIDelta{ToolCalls: indexed}, FinishReason: &fr},
	}, nil)
}

// MakeOpenAIStreamUsageChunk 独立的 usage 块。
//
// OpenAI 规范：stream_options.include_usage 时最后一个 chunk 携带 usage 且
// choices 为空数组。此前实现复用了收尾块（choices[0].finish_reason="stop"），
// 会在工具调用块之后把 finish_reason 覆盖成 stop，客户端因此认为模型正常结束
// 而不去执行工具调用——直接打断 agent 的工具循环（2026-09 审查发现）。
func MakeOpenAIStreamUsageChunk(id, model string, usage *OpenAIUsage) []byte {
	return marshalChunk(id, model, []OpenAIChoice{}, usage)
}

// MakeOpenAIResponse 创建 OpenAI 非流式响应
func MakeOpenAIResponse(model, content string) []byte {
	return MakeOpenAIResponseWithUsage(model, content, nil)
}

// MakeOpenAIResponseWithUsage 创建带 usage 的非流式响应
func MakeOpenAIResponseWithUsage(model, content string, usage *OpenAIUsage) []byte {
	now := time.Now().Unix()
	fr := "stop"
	resp := OpenAIChatResponse{
		ID:      fmt.Sprintf("chatcmpl-%s", uuid.New().String()[:8]),
		Object:  "chat.completion",
		Created: now,
		Model:   model,
		Choices: []OpenAIChoice{
			{
				Index: 0,
				Message: &OpenAIMessage{
					Role:    "assistant",
					Content: content,
				},
				FinishReason: &fr,
			},
		},
		Usage: usage,
	}
	data, _ := json.Marshal(resp)
	return data
}

// OpenAIModelsResponse 是 /v1/models 的响应
type OpenAIModelsResponse struct {
	Object string      `json:"object"`
	Data   interface{} `json:"data"`
}

// MakeOpenAIToolCallResponse 创建 OpenAI 非流式工具调用响应
func MakeOpenAIToolCallResponse(model string, toolCalls []OpenAIToolCall) []byte {
	now := time.Now().Unix()
	fr := "tool_calls"
	resp := OpenAIChatResponse{
		ID:      fmt.Sprintf("chatcmpl-%s", uuid.New().String()[:8]),
		Object:  "chat.completion",
		Created: now,
		Model:   model,
		Choices: []OpenAIChoice{
			{
				Index: 0,
				Message: &OpenAIMessage{
					Role:      "assistant",
					Content:   "",
					ToolCalls: toolCalls,
				},
				FinishReason: &fr,
			},
		},
	}
	data, _ := json.Marshal(resp)
	return data
}
