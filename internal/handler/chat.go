package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/teddyli18000/mimo-web-proxy/internal/adapter"
	"github.com/teddyli18000/mimo-web-proxy/internal/convstore"
	"github.com/teddyli18000/mimo-web-proxy/internal/mimo"
	"github.com/teddyli18000/mimo-web-proxy/internal/pool"
	"github.com/teddyli18000/mimo-web-proxy/internal/router"
	"github.com/teddyli18000/mimo-web-proxy/internal/stats"
	"github.com/teddyli18000/mimo-web-proxy/internal/toolcall"
)

type ChatHandler struct {
	pool      *pool.Pool
	convStore *convstore.Store
}

func NewChatHandler(p *pool.Pool, cs *convstore.Store) *ChatHandler {
	return &ChatHandler{pool: p, convStore: cs}
}

// parsedEvent 解析后的 SSE 事件
type parsedEvent struct {
	Event string
	Data  string
}

// usageData MiMo usage 事件结构
type usageData struct {
	PromptTokens     int `json:"promptTokens"`
	CompletionTokens int `json:"completionTokens"`
	TotalTokens      int `json:"totalTokens"`
	NativeUsage      *nativeUsage `json:"nativeUsage,omitempty"`
}

type nativeUsage struct {
	PromptTokens     int              `json:"prompt_tokens"`
	CompletionTokens int              `json:"completion_tokens"`
	TotalTokens      int              `json:"total_tokens"`
	PromptDetails    *promptDetails   `json:"prompt_tokens_details,omitempty"`
	CompletionDetails *completionDetails `json:"completion_tokens_details,omitempty"`
}

type promptDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

type completionDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

// dialogIdData dialogId 事件结构
type dialogIdData struct {
	Content string `json:"content"`
}

func (h *ChatHandler) Handle(w http.ResponseWriter, r *http.Request) {
	var req adapter.OpenAIChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	routeResult := router.RouteModel(req.Model, toMiMoMessages(req.Messages))
	log.Printf("[route] model=%s reason=%s", routeResult.Model, routeResult.Reason)

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()

	if !h.pool.HasAccounts() {
		writeError(w, http.StatusServiceUnavailable, "no accounts configured")
		return
	}

	h.handleWebChat(ctx, w, &req, routeResult.Model, req.Stream)
}

// handleWebChat 使用网页端反代 — 混合模式：
// query 组装 = 全量重放（system + 全部历史 + 当前消息，参考 meny2333/mimo2api_go 的
// serializeMessages 设计），解决 agent 客户端注入型 user 消息吞掉真实提问的问题；
// 会话连续性 = fingerprint 前缀延续判断（agent 循环追加历史 → 复用 MiMo 服务端上下文；
// 新会话 → 全新 conversationId）。
func (h *ChatHandler) handleWebChat(ctx context.Context, w http.ResponseWriter, req *adapter.OpenAIChatRequest, model string, stream bool) {
	client, err := h.pool.Next()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	// 组装 (role, text) 序列供 fingerprint 与 serialize 共用
	roleTexts := toRoleTexts(req.Messages)

	// 会话解析：fingerprint 前缀延续 → 复用；否则新建
	convID, parentID, isNew := h.convStore.Resolve(roleTexts)
	if isNew {
		log.Printf("[conv] new conversation %s (model=%s)", convID[:8], model)
	} else {
		log.Printf("[conv] continuing conversation %s (parentID=%s)", convID[:8], parentID[:min(len(parentID), 8)])
	}

	// 全量重放组装 query（serialize 会合并 system、渲染历史、高亮当前消息）
	query := serializeMessages(req.Messages)
	if query == "" {
		log.Printf("[filter] no valid user message")
		writeError(w, http.StatusBadRequest, "no valid user message")
		return
	}

	// Inject tool definitions into query so MiMo knows what tools are available
	if len(req.Tools) > 0 {
		toolPrompt := buildToolPrompt(req.Tools)
		query = toolPrompt + "\n\n" + query
		log.Printf("[tools] prompt with %d tools, query len=%d, convID=%s, parentID=%s",
			len(req.Tools), len(query), convID[:8], parentID[:min(len(parentID), 8)])
	}

	stats.Get().IncrConcurrency()
	defer stats.Get().DecrConcurrency()

	body, err := client.Chat(ctx, query, model, convID, parentID, false)
	if err != nil {
		log.Printf("[error] web chat: %v", err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("mimo error: %v", err))
		return
	}

	// 解析所有事件
	events := make(chan mimo.WebSSEEvent, 64)
	go func() {
		defer close(events)
		mimo.ParseWebSSE(ctx, body, events)
	}()

	// 分发事件：usage/dialogId 存入局部变量（fastchat 通道一次对话会发多个 usage
	// 事件——channel buffer 会被塞爆导致分发 goroutine 死锁，2026-09 实测），
	// 循环结束后经 usageResultChan 交给消费者；message 实时转发。
	lastUsage := new(*usageData)
	lastDialog := new(string)
	usageResultChan := make(chan *usageData, 1)
	lastMsgIDChan := make(chan string, 1)
	hasContentChan := make(chan bool, 1)
	msgChan := make(chan mimo.WebSSEEvent, 64)

	go func() {
		defer close(msgChan)
		lastMsgID := ""
		hasContent := false
		for ev := range events {
			if ev.Event == "message" && ev.ID != "" {
				lastMsgID = ev.ID
			}
			// Track if any non-empty text content was received
			if ev.Event == "message" && !hasContent {
				var check struct {
					Type    string `json:"type"`
					Content string `json:"content"`
				}
				if json.Unmarshal([]byte(ev.Data), &check) == nil && check.Type == "text" && check.Content != "" {
					hasContent = true
				}
			}
			switch ev.Event {
			case "usage":
				var u usageData
				if json.Unmarshal([]byte(ev.Data), &u) == nil {
					*lastUsage = &u // 覆盖：以最后一个 usage 为准（总额）
				}
			case "dialogId":
				var d dialogIdData
				if json.Unmarshal([]byte(ev.Data), &d) == nil {
					*lastDialog = d.Content
				}
			case "message":
				msgChan <- ev
			}
		}
		lastMsgIDChan <- lastMsgID
		close(lastMsgIDChan)
		hasContentChan <- hasContent
		close(hasContentChan)
		usageResultChan <- *lastUsage
		close(usageResultChan)
	}()

	// 流式/非流式输出（非流式返回响应体，usage 注入后再写出）
	var nonStreamBody []byte
	if stream {
		h.streamWebToOpenAI(w, model, msgChan, len(req.Tools) > 0)
	} else {
		nonStreamBody = h.nonStreamWebToOpenAI(w, model, msgChan)
	}

	// 后处理：取最终 usage —— 记录统计；流式在 [DONE] 前补 usage 收尾块；
	// 非流式的 usage 注入由 nonStreamWebToOpenAI 通过 usageResultChan 完成（见其实现）
	gotUsage := <-usageResultChan
	if u := gotUsage; u != nil {
		cached := 0
		reasoning := 0
		if u.NativeUsage != nil {
			if u.NativeUsage.PromptDetails != nil {
				cached = u.NativeUsage.PromptDetails.CachedTokens
			}
			if u.NativeUsage.CompletionDetails != nil {
				reasoning = u.NativeUsage.CompletionDetails.ReasoningTokens
			}
		}
		stats.Get().Record(model, u.PromptTokens, u.CompletionTokens, cached, reasoning, u.TotalTokens)
		log.Printf("[usage] model=%s prompt=%d completion=%d cached=%d reasoning=%d",
			model, u.PromptTokens, u.CompletionTokens, cached, reasoning)
		if stream {
			openaiUsage := &adapter.OpenAIUsage{
				PromptTokens:     u.PromptTokens,
				CompletionTokens: u.CompletionTokens,
				TotalTokens:      u.TotalTokens,
			}
			chunk := adapter.MakeOpenAIStreamChunkWithUsage(model, "", true, openaiUsage)
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			fmt.Fprintf(w, "data: [DONE]\n\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	} else if stream {
		// 无 usage 事件时也要补发 [DONE]（streamWebToOpenAI 只发到 finish 块）
		fmt.Fprintf(w, "data: [DONE]\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}

	// 非流式：注入 usage 后写出响应体
	if nonStreamBody != nil {
		if u := gotUsage; u != nil {
			var m map[string]interface{}
			if json.Unmarshal(nonStreamBody, &m) == nil {
				m["usage"] = map[string]int{
					"prompt_tokens":     u.PromptTokens,
					"completion_tokens": u.CompletionTokens,
					"total_tokens":      u.TotalTokens,
				}
				nonStreamBody, _ = json.Marshal(m)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(nonStreamBody)
	}

	// 保存对话到 MiMo 官网 + 更新 parentId（仅在有实际内容时）
	// ultra 对话的 save 也走 fastchat 通道
	hasContent := <-hasContentChan
	if convID != "" && hasContent {
		go client.SaveConversation(context.Background(), convID, query, model == router.ModelV26UltraSpeed)
	}
	if lastMsgID := <-lastMsgIDChan; lastMsgID != "" && hasContent {
		h.convStore.SetParentID(convID, lastMsgID)
		log.Printf("[conv] updated parentId for convID=%s: %s", convID[:8], lastMsgID[:min(len(lastMsgID), 8)])
	} else if !hasContent {
		log.Printf("[conv] empty response, keeping previous parentId for convID=%s", convID[:8])
	}
}

func (h *ChatHandler) streamWebToOpenAI(w http.ResponseWriter, model string, events <-chan mimo.WebSSEEvent, hasTools bool) {
	flusher := w.(http.Flusher)
	inThinking := false

	var buffered strings.Builder

	writeChunk := func(content string, finish bool) {
		chunk := adapter.MakeOpenAIStreamChunk(model, content, finish)
		fmt.Fprintf(w, "data: %s\n\n", chunk)
		flusher.Flush()
	}

	// 带 tools 的请求：先完整缓冲再决定输出形式。
	// 边收边发会导致"裸 DSML/tool_call 文本先流给客户端、tool_calls 块最后又发一次"
	// （客户端把工具调用语法当正文显示，2026-09 DSH 实测）。无 tools 时保持逐块实时流式。
	// 无 tools：直接发（不缓冲）
	// 有 tools：先缓冲，结束时若为 tool_calls 只发 tool_calls，否则一次性发正文
	for event := range events {
		var msg struct {
			Type    string `json:"type"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(event.Data), &msg); err != nil {
			continue
		}
		if msg.Type != "text" || msg.Content == "" {
			continue
		}
		c := strings.ReplaceAll(msg.Content, "\u0000", "")
		c, inThinking = filterThinkingChunk(c, inThinking)
		if c == "" {
			continue
		}
		buffered.WriteString(c)
		if !hasTools {
			// 无工具场景不存在工具语法歧义，逐块实时流式
			writeChunk(c, false)
		}
	}

	finalText := strings.TrimSpace(buffered.String())
	log.Printf("[tools] raw output (len=%d): %q", len(finalText), finalText[:min(len(finalText), 500)])
	if toolcall.HasToolCallSyntax(finalText) {
		calls := toolcall.ParseToolCallsFromText(finalText)
		log.Printf("[tools] parsed %d calls from text", len(calls))
		for i, c := range calls {
			log.Printf("[tools] call[%d]: name=%s input=%v", i, c.Name, c.Input)
		}
		if len(calls) > 0 {
			toolCalls := toolcall.ConvertToolCallsToOpenAI(calls)
			log.Printf("[tools] detected %d tool calls in stream", len(toolCalls))
			// 只发 tool_calls，不再把裸语法文本发给客户端
			toolChunk := adapter.MakeOpenAIStreamToolCallChunk(model, toolCalls, true)
			fmt.Fprintf(w, "data: %s\n\n", toolChunk)
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			return
		}
	}

	// 非 tool_calls：正文输出。带 tools 时此前一直在缓冲，这里一次性发出
	if s := buffered.String(); s != "" {
		writeChunk(s, false)
	}

	// 收尾 finish 块（usage 块与 [DONE] 由 handleWebChat 在拿到分发 goroutine
	// 的最终 usage 后统一发送——流式的 usage 块必须在 [DONE] 之前）
	writeChunk("", true)
}

// filterThinkingChunk 状态机方式过滤 thinking 内容
func filterThinkingChunk(content string, inThinking bool) (string, bool) {
	var result strings.Builder

	for len(content) > 0 {
		if inThinking {
			end := strings.Index(content, "</think>")
			if end == -1 {
				return "", true
			}
			content = content[end+8:]
			inThinking = false
			continue
		}

		start := strings.Index(content, "<think>")
		if start == -1 {
			result.WriteString(content)
			break
		}

		result.WriteString(content[:start])
		content = content[start+7:]
		inThinking = true
	}

	return result.String(), inThinking
}

func (h *ChatHandler) nonStreamWebToOpenAI(w http.ResponseWriter, model string, events <-chan mimo.WebSSEEvent) []byte {
	var content strings.Builder
	inThinking := false
	upstreamErr := ""

	for event := range events {
		switch event.Event {
		case "message":
			var msg struct {
				Type    string `json:"type"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal([]byte(event.Data), &msg); err != nil {
				continue
			}
			if msg.Type == "text" && msg.Content != "" {
				c := strings.ReplaceAll(msg.Content, "\u0000", "")
				c, inThinking = filterThinkingChunk(c, inThinking)
				content.WriteString(c)
			}
		case "error":
			var e struct {
				Type    string `json:"type"`
				Content string `json:"content"`
			}
			json.Unmarshal([]byte(event.Data), &e)
			if e.Content != "" {
				upstreamErr = e.Content
			}
		}
	}

	finalText := strings.TrimSpace(content.String())

	// 上游业务错误且没有任何正文 → 502 透传
	if upstreamErr != "" && finalText == "" {
		writeError(w, http.StatusBadGateway, "mimo: "+upstreamErr)
		return nil
	}

	// 检测是否包含工具调用
	log.Printf("[tools] non-stream raw output (len=%d): %q", len(finalText), finalText[:min(len(finalText), 500)])
	if toolcall.HasToolCallSyntax(finalText) {
		calls := toolcall.ParseToolCallsFromText(finalText)
		log.Printf("[tools] non-stream parsed %d calls", len(calls))
		for i, c := range calls {
			log.Printf("[tools] call[%d]: name=%s input=%v", i, c.Name, c.Input)
		}
		if len(calls) > 0 {
			toolCalls := toolcall.ConvertToolCallsToOpenAI(calls)
			log.Printf("[tools] detected %d tool calls in response", len(toolCalls))
			// 返回响应体，由 handleWebChat 注入 usage 后统一写出
			return adapter.MakeOpenAIToolCallResponse(model, toolCalls)
		}
	}

	// 非流式响应体（usage 由 handleWebChat 拿到分发 goroutine 的最终值后注入）
	return adapter.MakeOpenAIResponse(model, finalText)
}

func toMiMoMessages(msgs []adapter.OpenAIMessage) []mimo.Message {
	result := make([]mimo.Message, len(msgs))
	for i, m := range msgs {
		result[i] = mimo.Message{Role: m.Role, Content: m.Content}
	}
	return result
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"message": msg,
			"type":    "invalid_request_error",
			"code":    status,
		},
	})
}

func ModelsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"object": "list",
		"data":   router.SupportedModels(),
	})
}

// MessagesHandler Anthropic 格式
type MessagesHandler struct {
	pool      *pool.Pool
	convStore *convstore.Store
}

func NewMessagesHandler(p *pool.Pool, cs *convstore.Store) *MessagesHandler {
	return &MessagesHandler{pool: p, convStore: cs}
}

func (h *MessagesHandler) Handle(w http.ResponseWriter, r *http.Request) {
	var req adapter.AnthropicRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}

	routeResult := router.RouteModel(req.Model, nil)

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()

	if !h.pool.HasAccounts() {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "no accounts configured"})
		return
	}

	client, err := h.pool.Next()
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// 组装 (role, text) 序列供 fingerprint 与 serialize 共用
	roleTexts := toRoleTextsAnthropic(req.Messages, req.System)

	// 会话解析：fingerprint 前缀延续 → 复用；否则新建
	convID, parentID, isNew := h.convStore.Resolve(roleTexts)
	if isNew {
		log.Printf("[conv] Anthropic new conversation %s (model=%s)", convID[:8], routeResult.Model)
	} else {
		log.Printf("[conv] Anthropic continuing conversation %s (parentID=%s)", convID[:8], parentID[:min(len(parentID), 8)])
	}

	// 全量重放组装 query
	query := serializeMessagesAnthropic(req.Messages, req.System)
	if query == "" {
		log.Printf("[filter] no valid Anthropic user message")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "no valid user message"})
		return
	}

	// Inject tool definitions into query so MiMo knows what tools are available
	hasTools := len(req.Tools) > 0
	if hasTools {
		openaiTools := adapter.ConvertAnthropicToolsToOpenAI(req.Tools)
		toolPrompt := buildToolPrompt(openaiTools)
		query = toolPrompt + "\n\n" + query
		log.Printf("[tools] Anthropic prompt with %d tools, query len=%d, convID=%s, parentID=%s",
			len(req.Tools), len(query), convID[:8], parentID[:min(len(parentID), 8)])
	}

	stats.Get().IncrConcurrency()
	defer stats.Get().DecrConcurrency()

	body, err := client.Chat(ctx, query, routeResult.Model, convID, parentID, false)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	events := make(chan mimo.WebSSEEvent, 64)
	go func() {
		defer close(events)
		mimo.ParseWebSSE(ctx, body, events)
	}()

	// 分发事件（同 OpenAI 路径：usage 存局部变量，防 fastchat 多 usage 事件死锁）
	lastUsageA := new(*usageData)
	msgChan := make(chan mimo.WebSSEEvent, 64)
	lastMsgIDChan := make(chan string, 1)
	hasContentChan := make(chan bool, 1)

	go func() {
		defer close(msgChan)
		lastMsgID := ""
		hasContent := false
		for ev := range events {
			if ev.Event == "message" && ev.ID != "" {
				lastMsgID = ev.ID
			}
			// Track if any non-empty text content was received
			if ev.Event == "message" && !hasContent {
				var check struct {
					Type    string `json:"type"`
					Content string `json:"content"`
				}
				if json.Unmarshal([]byte(ev.Data), &check) == nil && check.Type == "text" && check.Content != "" {
					hasContent = true
				}
			}
			switch ev.Event {
			case "usage":
				var u usageData
				if json.Unmarshal([]byte(ev.Data), &u) == nil {
					*lastUsageA = &u
				}
			case "message":
				msgChan <- ev
			}
		}
		lastMsgIDChan <- lastMsgID
		close(lastMsgIDChan)
		hasContentChan <- hasContent
		close(hasContentChan)
	}()

	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		inThinking := false
		var buffered strings.Builder

		// Send message_start event
		startMsg := map[string]interface{}{
			"type": "message_start",
			"message": map[string]interface{}{
				"id":    fmt.Sprintf("msg_%s", uuid.New().String()[:24]),
				"type":  "message",
				"role":  "assistant",
				"model": routeResult.Model,
			},
		}
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", adapter.MakeAnthropicStreamEvent("message_start", startMsg))
		// Send content_block_start for text block (index 0)
		textBlockStart := map[string]interface{}{
			"type":         "content_block_start",
			"index":        0,
			"content_block": map[string]interface{}{"type": "text", "text": ""},
		}
		fmt.Fprintf(w, "event: content_block_start\ndata: %s\n\n", adapter.MakeAnthropicStreamEvent("content_block_start", textBlockStart))
		flusher.Flush()

		for event := range msgChan {
			if event.Event != "message" {
				continue
			}
			var msg struct {
				Type    string `json:"type"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal([]byte(event.Data), &msg); err != nil {
				continue
			}
			if msg.Type == "text" && msg.Content != "" {
				c := strings.ReplaceAll(msg.Content, "\u0000", "")
				c, inThinking = filterThinkingChunk(c, inThinking)
				if c != "" {
					buffered.WriteString(c)
					// Always stream text chunks immediately
					delta := adapter.AnthropicTextDelta{Type: "text_delta", Text: c}
					fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", adapter.MakeAnthropicStreamEvent("content_block_delta", delta))
					flusher.Flush()
				}
			}
		}
		// Close text content block
		fmt.Fprintf(w, "event: content_block_stop\ndata: %s\n\n", adapter.MakeAnthropicStreamEvent("content_block_stop", map[string]interface{}{"type": "content_block_stop", "index": 0}))

		// Check for tool calls in buffered output
		finalText := strings.TrimSpace(buffered.String())
		if hasTools && toolcall.HasToolCallSyntax(finalText) {
			calls := toolcall.ParseToolCallsFromText(finalText)
			log.Printf("[tools] Anthropic stream: parsed %d calls from text", len(calls))
			if len(calls) > 0 {
				// Send tool_use blocks as streaming events
				for i, c := range calls {
					blockIdx := i + 1 // text block is index 0
					block := adapter.AnthropicToolUseBlock{
						Type:  "tool_use",
						ID:    "toolu_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:24],
						Name:  c.Name,
						Input: c.Input,
					}
					toolStart := map[string]interface{}{
						"type":  "content_block_start",
						"index": blockIdx,
						"content_block": map[string]interface{}{
							"type":  "tool_use",
							"id":    block.ID,
							"name":  block.Name,
							"input": block.Input,
						},
					}
					fmt.Fprintf(w, "event: content_block_start\ndata: %s\n\n", adapter.MakeAnthropicStreamEvent("content_block_start", toolStart))
					fmt.Fprintf(w, "event: content_block_stop\ndata: %s\n\n", adapter.MakeAnthropicStreamEvent("content_block_stop", map[string]interface{}{"type": "content_block_stop", "index": blockIdx}))
				}
				fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", adapter.MakeAnthropicStreamEvent("message_delta", map[string]interface{}{
					"type":        "message_delta",
					"stop_reason": "tool_use",
				}))
			}
		} else {
			fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", adapter.MakeAnthropicStreamEvent("message_delta", map[string]interface{}{
				"type":        "message_delta",
				"stop_reason": "end_turn",
			}))
		}
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", adapter.MakeAnthropicStreamEvent("message_stop", nil))
		flusher.Flush()
	} else {
		var content strings.Builder
		inThinking := false
		for event := range msgChan {
			if event.Event == "message" {
				var msg struct {
					Type    string `json:"type"`
					Content string `json:"content"`
				}
				if err := json.Unmarshal([]byte(event.Data), &msg); err == nil && msg.Type == "text" {
					c := strings.ReplaceAll(msg.Content, "\u0000", "")
					c, inThinking = filterThinkingChunk(c, inThinking)
					content.WriteString(c)
				}
			}
		}
		finalText := strings.TrimSpace(content.String())
		// Check for tool calls in the response
		if hasTools && toolcall.HasToolCallSyntax(finalText) {
			calls := toolcall.ParseToolCallsFromText(finalText)
			log.Printf("[tools] Anthropic: parsed %d calls from text", len(calls))
			if len(calls) > 0 {
				// Return Anthropic format with tool_use blocks
				blocks := make([]interface{}, 0, len(calls))
				for _, c := range calls {
					blocks = append(blocks, adapter.AnthropicToolUseBlock{
						Type:  "tool_use",
						ID:    "toolu_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:24],
						Name:  c.Name,
						Input: c.Input,
					})
				}
				resp := map[string]interface{}{
					"id":          fmt.Sprintf("msg_%s", uuid.New().String()[:24]),
					"type":        "message",
					"role":        "assistant",
					"content":     blocks,
					"model":       routeResult.Model,
					"stop_reason": "tool_use",
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			} else {
				resp := adapter.MakeAnthropicResponse(routeResult.Model, finalText)
				w.Header().Set("Content-Type", "application/json")
				w.Write(resp)
			}
		} else {
			resp := adapter.MakeAnthropicResponse(routeResult.Model, finalText)
			w.Header().Set("Content-Type", "application/json")
			w.Write(resp)
		}
	}

	// 记录 usage
	if u := *lastUsageA; u != nil {
		cached := 0
		reasoning := 0
		if u.NativeUsage != nil {
			if u.NativeUsage.PromptDetails != nil {
				cached = u.NativeUsage.PromptDetails.CachedTokens
			}
			if u.NativeUsage.CompletionDetails != nil {
				reasoning = u.NativeUsage.CompletionDetails.ReasoningTokens
			}
		}
		stats.Get().Record(routeResult.Model, u.PromptTokens, u.CompletionTokens, cached, reasoning, u.TotalTokens)
	}

	// 更新 parentId + 同步网页端历史（仅在有实际内容时）
	hasContent := <-hasContentChan
	if lastMsgID := <-lastMsgIDChan; lastMsgID != "" && hasContent {
		h.convStore.SetParentID(convID, lastMsgID)
		log.Printf("[conv] Anthropic: updated parentId for convID=%s: %s", convID[:8], lastMsgID[:min(len(lastMsgID), 8)])
	} else if !hasContent {
		log.Printf("[conv] Anthropic: empty response, keeping previous parentId for convID=%s", convID[:8])
	}
	// 保存对话到 MiMo 官网历史记录
	if hasContent {
		go client.SaveConversation(context.Background(), convID, query, routeResult.Model == router.ModelV26UltraSpeed)
	}
}

// extractFirstOpenAIUserMessage extracts the first user message from OpenAI messages.
func extractFirstOpenAIUserMessage(msgs []adapter.OpenAIMessage) string {
	for _, msg := range msgs {
		if msg.Role == "user" {
			if s, ok := msg.Content.(string); ok {
				return s
			}
		}
	}
	return ""
}

// extractLatestOpenAIUserMessage extracts the latest user message from OpenAI messages.
// Skips auto-generated messages like "predict next message" from MiMo Code.
func extractLatestOpenAIUserMessage(msgs []adapter.OpenAIMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			if s, ok := msgs[i].Content.(string); ok {
				if isAutoGeneratedQuery(s) {
					log.Printf("[filter] skipping auto-generated query (len=%d): %q", len(s), s[:min(len(s), 100)])
					continue
				}
				return s
			}
		}
	}
	return ""
}

// isAutoGeneratedQuery detects auto-generated messages from MiMo Code features
// like "predict next message" that should not be forwarded to MiMo.
func isAutoGeneratedQuery(s string) bool {
	lower := strings.ToLower(s)
	autoPatterns := []string{
		"based on the conversation above",
		"write the user's most likely next message",
		"most likely next message",
		"user's most likely",
		"above conversation",
	}
	for _, p := range autoPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	// Chinese patterns
	zhPatterns := []string{
		"根据以上对话",
		"根据对话上下文",
		"最可能发送的下一条消息",
		"预测用户的下一条消息",
		"写出用户最可能",
		"用户最可能的下一条",
		"用户最可能发送",
	}
	for _, p := range zhPatterns {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

