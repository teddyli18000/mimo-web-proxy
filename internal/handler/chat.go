package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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

// usageData MiMo usage 事件结构
type usageData struct {
	PromptTokens     int          `json:"promptTokens"`
	CompletionTokens int          `json:"completionTokens"`
	TotalTokens      int          `json:"totalTokens"`
	NativeUsage      *nativeUsage `json:"nativeUsage,omitempty"`
}

type nativeUsage struct {
	PromptTokens      int                `json:"prompt_tokens"`
	CompletionTokens  int                `json:"completion_tokens"`
	TotalTokens       int                `json:"total_tokens"`
	PromptDetails     *promptDetails     `json:"prompt_tokens_details,omitempty"`
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

// webResult 收集上游 MiMo 网页端 SSE 流的完整结果
type webResult struct {
	Text        string     // 过滤 think 与 \u0000 后的全部正文
	Usage       *usageData // 最后一个 usage 事件（fastchat 会发多个，取最后一个）
	LastMsgID   string     // 最后一个带 id 的事件 id
	UpstreamErr string     // event:error 的 content
}

func isWebResultEmpty(r webResult) bool {
	return r.Text == "" && r.UpstreamErr == ""
}

func randomHex32() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func collectWebResult(ctx context.Context, client *mimo.WebClient, query, model, convID, parentID string) (webResult, error) {
	body, err := client.Chat(ctx, query, model, convID, parentID, false)
	if err != nil {
		return webResult{}, err
	}
	return collectWebResultFromReader(ctx, body)
}

func collectWebResultFromReader(ctx context.Context, reader io.ReadCloser) (webResult, error) {
	events := make(chan mimo.WebSSEEvent, 64)
	errChan := make(chan error, 1)
	go func() {
		defer close(events)
		errChan <- mimo.ParseWebSSE(ctx, reader, events)
	}()

	var (
		content     strings.Builder
		inThinking  bool
		lastUsage   *usageData
		lastMsgID   string
		upstreamErr string
	)

	for ev := range events {
		switch ev.Event {
		case "message":
			if ev.ID != "" {
				lastMsgID = ev.ID
			}
			var msg struct {
				Type    string `json:"type"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &msg); err != nil {
				continue
			}
			if msg.Type == "text" && msg.Content != "" {
				c := strings.ReplaceAll(msg.Content, "\u0000", "")
				c, inThinking = filterThinkingChunk(c, inThinking)
				if c != "" {
					content.WriteString(c)
				}
			}
		case "error":
			var e struct {
				Type    string `json:"type"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &e); err == nil && e.Content != "" {
				upstreamErr = e.Content
			}
		case "usage":
			var u usageData
			if err := json.Unmarshal([]byte(ev.Data), &u); err == nil {
				lastUsage = &u // 覆盖：以最后一个 usage 为准（fastchat 多 usage 事件规避）
			}
		}
	}

	if err := <-errChan; err != nil && ctx.Err() != nil {
		return webResult{}, err
	}

	return webResult{
		Text:        content.String(),
		Usage:       lastUsage,
		LastMsgID:   lastMsgID,
		UpstreamErr: upstreamErr,
	}, nil
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
// 收集策略 = 先完整收集上游响应，检测到空响应自动重试一次（换全新 conversationId）。
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

	// query 组装策略：
	//   新会话 → 完整上下文（system + 历史 + 当前消息）+ 完整工具 schema
	//   延续会话 → 仅增量消息（上游服务端已有上下文）+ 紧凑工具名提醒
	// 增量发送与网页端原生行为一致：缩短 query、避免长度限制、降低每轮开销。
	//
	// 工具结果轮（本轮没有新的用户提问）必须带上任务锚点：只发工具输出时模型会
	// 丢失任务（2026-09 DSH 实测，模型自述"只收到工具输出、没有原始指令"）。
	budget := router.MaxQueryCharsForModel(model)
	turnQuestion := currentTurnQuestion(req.Messages)
	if turnQuestion != "" {
		h.convStore.SetTask(convID, turnQuestion)
	}
	taskAnchor := h.convStore.Task(convID)
	// 工具结果轮（本轮没有新提问）需要把任务锚点带回去
	useAnchor := !isNew && turnQuestion == "" && taskAnchor != ""

	buildQuery := func(full bool) string {
		msgs := req.Messages
		if !full {
			msgs = DeltaMessages(req.Messages)
		}
		var extra []string
		if len(req.Tools) > 0 {
			if full {
				extra = append(extra, buildToolPrompt(req.Tools))
			} else if r := buildToolReminder(req.Tools); r != "" {
				extra = append(extra, r)
			}
		}
		if !full && useAnchor {
			extra = append(extra, "Current task (the user's request you are working on): "+taskAnchor)
		}
		return serializeMessages(msgs, budget, extra...)
	}

	query := buildQuery(isNew)
	if query == "" {
		query = buildQuery(true) // 增量异常时退化为全量
	}
	if query == "" {
		log.Printf("[filter] no valid user message")
		writeError(w, http.StatusBadRequest, "no valid user message")
		return
	}
	anchorNote := ""
	if useAnchor {
		anchorNote = fmt.Sprintf(" anchor=%d", len(taskAnchor))
	}
	log.Printf("[query] len=%d budget=%d mode=%s%s", len(query), budget,
		map[bool]string{true: "full", false: "delta"}[isNew], anchorNote)

	stats.Get().IncrConcurrency()
	defer stats.Get().DecrConcurrency()

	curConvID := convID
	curParentID := parentID
	var result webResult

	for attempt := 0; attempt < 2; attempt++ {
		var err error
		result, err = collectWebResult(ctx, client, query, model, curConvID, curParentID)
		if err != nil {
			log.Printf("[error] web chat (attempt %d): %v", attempt+1, err)
			writeError(w, http.StatusBadGateway, fmt.Sprintf("mimo error: %v", err))
			return
		}

		// 上游明确业务错误（如文本超长）且无有效正文 → 不重试直接报错
		if result.UpstreamErr != "" && result.Text == "" {
			log.Printf("[error] mimo upstream error: %s", result.UpstreamErr)
			writeError(w, http.StatusBadGateway, "mimo: "+result.UpstreamErr)
			return
		}

		// 空响应重试：首次尝试若为空且无明确错误，换全新 conversationId 重试一次。
		// 重试必须改用【完整上下文】——新会话没有服务端历史，只发增量会答非所问。
		if isWebResultEmpty(result) {
			if attempt == 0 {
				log.Printf("[retry] empty response on convID=%s, retrying with full context", curConvID[:min(len(curConvID), 8)])
				curConvID = randomHex32()
				curParentID = "0"
				query = buildQuery(true)
				continue
			}
			log.Printf("[error] mimo returned an empty response after retry")
			writeError(w, http.StatusBadGateway, "mimo returned an empty response (after retry)")
			return
		}

		break
	}

	// 记录 usage
	var openaiUsage *adapter.OpenAIUsage
	if u := result.Usage; u != nil {
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
		openaiUsage = &adapter.OpenAIUsage{
			PromptTokens:     u.PromptTokens,
			CompletionTokens: u.CompletionTokens,
			TotalTokens:      u.TotalTokens,
		}
	}

	// 保存对话到 MiMo 官网 + 更新 parentId（仅在有实际内容时）
	if curConvID != "" && result.Text != "" {
		go client.SaveConversation(context.Background(), curConvID, query, model == router.ModelV26UltraSpeed)
	}
	if result.LastMsgID != "" && result.Text != "" {
		h.convStore.SetParentID(curConvID, result.LastMsgID)
		log.Printf("[conv] updated parentId for convID=%s: %s", curConvID[:8], result.LastMsgID[:min(len(result.LastMsgID), 8)])
	}

	// 检测是否包含工具调用
	finalText := strings.TrimSpace(result.Text)
	if toolcall.HasToolCallSyntax(finalText) {
		calls := toolcall.ParseToolCallsFromText(finalText)
		log.Printf("[tools] parsed %d calls from text", len(calls))
		for i, c := range calls {
			log.Printf("[tools] call[%d]: name=%s input=%v", i, c.Name, c.Input)
		}
		if len(calls) > 0 {
			toolCalls := toolcall.ConvertToolCallsToOpenAI(calls)
			log.Printf("[tools] detected %d tool calls in response", len(toolCalls))
			if stream {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Cache-Control", "no-cache")
				streamID := adapter.NewStreamID()
				fmt.Fprintf(w, "data: %s\n\n", adapter.MakeOpenAIStreamToolCallChunk(streamID, model, toolCalls))
				if openaiUsage != nil {
					fmt.Fprintf(w, "data: %s\n\n", adapter.MakeOpenAIStreamUsageChunk(streamID, model, openaiUsage))
				}
				fmt.Fprintf(w, "data: [DONE]\n\n")
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
				return
			} else {
				respBody := adapter.MakeOpenAIToolCallResponse(model, toolCalls)
				if openaiUsage != nil {
					var m map[string]interface{}
					if json.Unmarshal(respBody, &m) == nil {
						m["usage"] = openaiUsage
						respBody, _ = json.Marshal(m)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write(respBody)
				return
			}
		}
	}

	// 输出正文
	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		streamID := adapter.NewStreamID()
		fmt.Fprintf(w, "data: %s\n\n", adapter.MakeOpenAIStreamContentChunk(streamID, model, result.Text))
		fmt.Fprintf(w, "data: %s\n\n", adapter.MakeOpenAIStreamFinishChunk(streamID, model))
		if openaiUsage != nil {
			fmt.Fprintf(w, "data: %s\n\n", adapter.MakeOpenAIStreamUsageChunk(streamID, model, openaiUsage))
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	} else {
		respBody := adapter.MakeOpenAIResponseWithUsage(model, result.Text, openaiUsage)
		w.Header().Set("Content-Type", "application/json")
		w.Write(respBody)
	}
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

// writeAnthropicError 按 Anthropic 规范返回错误：
//
//	{"type":"error","error":{"type":"invalid_request_error","message":"…"}}
//
// 此前返回 {"error":"…"} 且未设置 Content-Type，官方 SDK 无法取到 err.type
// 与 err.message（2026-09 协议审查发现）。
func writeAnthropicError(w http.ResponseWriter, status int, msg string) {
	errType := "api_error"
	switch {
	case status == http.StatusBadRequest:
		errType = "invalid_request_error"
	case status == http.StatusUnauthorized:
		errType = "authentication_error"
	case status == http.StatusTooManyRequests:
		errType = "rate_limit_error"
	case status == http.StatusServiceUnavailable:
		errType = "overloaded_error"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"type": "error",
		"error": map[string]string{
			"type":    errType,
			"message": msg,
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
		writeAnthropicError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	routeResult := router.RouteModel(req.Model, nil)

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()

	if !h.pool.HasAccounts() {
		writeAnthropicError(w, http.StatusServiceUnavailable, "no accounts configured")
		return
	}

	client, err := h.pool.Next()
	if err != nil {
		writeAnthropicError(w, http.StatusServiceUnavailable, err.Error())
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

	// query 组装：新会话完整上下文 + 完整工具 schema；延续会话仅增量 + 紧凑工具名提醒。
	// 与 OpenAI 路径一致，工具结果轮要带上任务锚点。
	hasTools := len(req.Tools) > 0
	budgetA := router.MaxQueryCharsForModel(routeResult.Model)
	turnQuestionA := currentTurnQuestionAnthropic(req.Messages)
	if turnQuestionA != "" {
		h.convStore.SetTask(convID, turnQuestionA)
	}
	taskAnchorA := h.convStore.Task(convID)
	useAnchorA := !isNew && turnQuestionA == "" && taskAnchorA != ""

	buildQueryA := func(full bool) string {
		msgs := req.Messages
		if !full {
			msgs = DeltaMessagesAnthropic(req.Messages)
		}
		var extra []string
		if hasTools {
			openaiTools := adapter.ConvertAnthropicToolsToOpenAI(req.Tools)
			if full {
				extra = append(extra, buildToolPrompt(openaiTools))
			} else if r := buildToolReminder(openaiTools); r != "" {
				extra = append(extra, r)
			}
		}
		if !full && useAnchorA {
			extra = append(extra, "Current task (the user's request you are working on): "+taskAnchorA)
		}
		return serializeMessagesAnthropic(msgs, req.System, budgetA, extra...)
	}
	query := buildQueryA(isNew)
	if query == "" {
		query = buildQueryA(true)
	}
	if query == "" {
		log.Printf("[filter] no valid Anthropic user message")
		writeAnthropicError(w, http.StatusBadRequest, "no valid user message")
		return
	}
	anchorNoteA := ""
	if useAnchorA {
		anchorNoteA = fmt.Sprintf(" anchor=%d", len(taskAnchorA))
	}
	log.Printf("[query] Anthropic len=%d budget=%d mode=%s%s", len(query), budgetA,
		map[bool]string{true: "full", false: "delta"}[isNew], anchorNoteA)

	stats.Get().IncrConcurrency()
	defer stats.Get().DecrConcurrency()

	curConvID := convID
	curParentID := parentID
	var result webResult

	for attempt := 0; attempt < 2; attempt++ {
		var err error
		result, err = collectWebResult(ctx, client, query, routeResult.Model, curConvID, curParentID)
		if err != nil {
			log.Printf("[error] Anthropic web chat (attempt %d): %v", attempt+1, err)
			writeAnthropicError(w, http.StatusBadGateway, err.Error())
			return
		}

		// 上游明确业务错误（如文本超长）且无有效正文 → 不重试直接报错
		if result.UpstreamErr != "" && result.Text == "" {
			log.Printf("[error] Anthropic mimo upstream error: %s", result.UpstreamErr)
			writeAnthropicError(w, http.StatusBadGateway, "mimo: " + result.UpstreamErr)
			return
		}

		// 空响应重试：首次尝试若为空且无明确错误，换全新 conversationId 重试一次
		if isWebResultEmpty(result) {
			if attempt == 0 {
				log.Printf("[retry] Anthropic empty response on convID=%s, retrying with new convID...", curConvID[:min(len(curConvID), 8)])
				curConvID = randomHex32()
				curParentID = "0"
				continue
			}
			log.Printf("[error] Anthropic mimo returned an empty response after retry")
			writeAnthropicError(w, http.StatusBadGateway, "mimo returned an empty response (after retry)")
			return
		}

		break
	}

	// 记录 usage
	inTokens, outTokens := 0, 0
	if u := result.Usage; u != nil {
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
		inTokens = u.PromptTokens
		outTokens = u.CompletionTokens
	}

	// 保存对话到 MiMo 官网历史记录 + 更新 parentId（仅在有实际内容时）
	if curConvID != "" && result.Text != "" {
		go client.SaveConversation(context.Background(), curConvID, query, routeResult.Model == router.ModelV26UltraSpeed)
	}
	if result.LastMsgID != "" && result.Text != "" {
		h.convStore.SetParentID(curConvID, result.LastMsgID)
		log.Printf("[conv] Anthropic: updated parentId for convID=%s: %s", curConvID[:8], result.LastMsgID[:min(len(result.LastMsgID), 8)])
	}

	finalText := strings.TrimSpace(result.Text)
	if hasTools && toolcall.HasToolCallSyntax(finalText) {
		calls := toolcall.ParseToolCallsFromText(finalText)
		log.Printf("[tools] Anthropic: parsed %d calls from text", len(calls))
		if len(calls) > 0 {
			if req.Stream {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Cache-Control", "no-cache")
				w.WriteHeader(http.StatusOK)
				flusher, _ := w.(http.Flusher)

				// Send message_start event（Anthropic 协议要求含 content/stop_reason/usage）
				startMsg := map[string]interface{}{
					"type": "message_start",
					"message": map[string]interface{}{
						"id":            fmt.Sprintf("msg_%s", uuid.New().String()[:24]),
						"type":          "message",
						"role":          "assistant",
						"model":         routeResult.Model,
						"content":       []interface{}{},
						"stop_reason":   nil,
						"stop_sequence": nil,
						"usage":         map[string]interface{}{"input_tokens": inTokens, "output_tokens": 0},
					},
				}
				fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", adapter.MakeAnthropicStreamEvent("message_start", startMsg))

				// Send tool_use blocks as streaming events
				for i, c := range calls {
					blockIdx := i
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
					"type": "message_delta",
					"delta": map[string]interface{}{
						"stop_reason":   "tool_use",
						"stop_sequence": nil,
					},
					"usage": map[string]interface{}{"output_tokens": outTokens},
				}))
				fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", adapter.MakeAnthropicStreamEvent("message_stop", nil))
				if flusher != nil {
					flusher.Flush()
				}
				return
			} else {
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
					"usage":       map[string]interface{}{"input_tokens": inTokens, "output_tokens": outTokens},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
				return
			}
		}
	}

	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)

		// Send message_start event（Anthropic 协议要求含 content/stop_reason/usage）
		startMsg := map[string]interface{}{
			"type": "message_start",
			"message": map[string]interface{}{
				"id":            fmt.Sprintf("msg_%s", uuid.New().String()[:24]),
				"type":          "message",
				"role":          "assistant",
				"model":         routeResult.Model,
				"content":       []interface{}{},
				"stop_reason":   nil,
				"stop_sequence": nil,
				"usage":         map[string]interface{}{"input_tokens": inTokens, "output_tokens": 0},
			},
		}
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", adapter.MakeAnthropicStreamEvent("message_start", startMsg))
		// Send content_block_start for text block (index 0)
		textBlockStart := map[string]interface{}{
			"type":          "content_block_start",
			"index":         0,
			"content_block": map[string]interface{}{"type": "text", "text": ""},
		}
		fmt.Fprintf(w, "event: content_block_start\ndata: %s\n\n", adapter.MakeAnthropicStreamEvent("content_block_start", textBlockStart))
		if result.Text != "" {
			fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", adapter.MakeAnthropicStreamEvent("content_block_delta", map[string]interface{}{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]interface{}{"type": "text_delta", "text": result.Text},
			}))
		}
		// Close text content block
		fmt.Fprintf(w, "event: content_block_stop\ndata: %s\n\n", adapter.MakeAnthropicStreamEvent("content_block_stop", map[string]interface{}{"type": "content_block_stop", "index": 0}))
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", adapter.MakeAnthropicStreamEvent("message_delta", map[string]interface{}{
			"type": "message_delta",
			"delta": map[string]interface{}{
				"stop_reason":   "end_turn",
				"stop_sequence": nil,
			},
			"usage": map[string]interface{}{"output_tokens": outTokens},
		}))
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", adapter.MakeAnthropicStreamEvent("message_stop", nil))
		if flusher != nil {
			flusher.Flush()
		}
	} else {
		resp := adapter.MakeAnthropicResponseWithUsage(routeResult.Model, finalText, inTokens, outTokens)
		w.Header().Set("Content-Type", "application/json")
		w.Write(resp)
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
