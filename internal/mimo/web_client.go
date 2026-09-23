package mimo

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	webBaseURL = "https://aistudio.xiaomimimo.com"
	chatAPI    = "/open-apis/bot/chat"
	// ultraspeed 型号走独立的 fastchat 通道（2026-09 用户真实抓包证实；
	// 在 open-apis 通道发送该型号会返回"模型名称错误"）
	ultraChatAPI    = "/fastchat/open-apis/bot/chat"
	ultraSaveAPI    = "/fastchat/open-apis/chat/conversation/save"
	modelUltraSpeed = "mimo-v2.6-pro-ultraspeed-studio"

	// 浏览器指纹：2026-09 Chrome 稳定版。上游按 UA/sec-ch-ua 做一致性风控，
	// 过期版本会被拒。改这里时 UA 与 sec-ch-ua 的版本号必须同步。
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/153.0.0.0 Safari/537.36"
	secChUA   = "\"Google Chrome\";v=\"153\", \"Chromium\";v=\"153\", \"Not)A;Brand\";v=\"24\""
)

// setBrowserHeaders 注入完整的浏览器请求头（Chat / Save / Validate 共用）
func setBrowserHeaders(h http.Header) {
	h.Set("Content-Type", "application/json")
	h.Set("Origin", webBaseURL)
	h.Set("Referer", webBaseURL+"/")
	h.Set("x-timezone", "Asia/Shanghai")
	h.Set("User-Agent", userAgent)
	h.Set("sec-ch-ua", secChUA)
	h.Set("sec-ch-ua-mobile", "?0")
	h.Set("sec-ch-ua-platform", "\"Windows\"")
	h.Set("sec-fetch-dest", "empty")
	h.Set("sec-fetch-mode", "cors")
	h.Set("sec-fetch-site", "same-origin")
}

// buildCookie 构造与浏览器完全一致的 Cookie 头。
// 2026-09 实测（用户 DevTools）：上游的 serviceToken cookie 名带 xiaomichatbot_ 前缀，
// 值外层带双引号（DevTools Cookie 面板显示 "..."）。
// 双发两种名字 + 保留引号形态，最大化兼容新旧两种会话格式。
func (c *WebClient) buildCookie() string {
	token := strings.Trim(c.serviceToken, "\"")
	ph := strings.Trim(c.ph, "\"")
	return fmt.Sprintf(
		"userId=%s; serviceToken=%s; xiaomichatbot_serviceToken=%q; xiaomichatbot_ph=%q",
		c.userID, token, token, ph,
	)
}

// WebClient 是 MiMo AI Studio 网页端客户端
type WebClient struct {
	httpClient *http.Client
	serviceToken string
	userID      string
	ph          string
}

// NewWebClient 创建网页端客户端
func NewWebClient(serviceToken, userID, ph string) *WebClient {
	return &WebClient{
		httpClient:   &http.Client{Timeout: 30 * time.Minute},
		serviceToken: serviceToken,
		userID:       userID,
		ph:           ph,
	}
}

// WebChatRequest 是网页端请求格式（字段与 2026-09 浏览器抓包一致：
// 32 位 hex 的 msgId/conversationId，仅 query+isEditedQuery+modelConfig+multiMedias）
type WebChatRequest struct {
	MsgID          string       `json:"msgId"`
	ConversationID string       `json:"conversationId"`
	Query          string       `json:"query"`
	IsEditedQuery  bool         `json:"isEditedQuery"`
	ModelConfig    ModelConfig  `json:"modelConfig"`
	MultiMedias    []interface{} `json:"multiMedias"`
}

// ModelConfig 模型配置
type ModelConfig struct {
	EnableThinking  bool    `json:"enableThinking"`
	WebSearchStatus string  `json:"webSearchStatus"`
	Model           string  `json:"model"`
	Temperature     float64 `json:"temperature"`
	TopP            float64 `json:"topP"`
}

// Chat 发起聊天，返回 SSE 流
// conversationID: 客户端提供的对话 ID（32 位 hex），用于复用 MiMo 服务端上下文
// parentID: 保留参数（当前协议请求体不需要，仅用于将来兼容）
func (c *WebClient) Chat(ctx context.Context, query, model, conversationID, parentID string, thinking bool) (io.ReadCloser, error) {
	if conversationID == "" {
		conversationID = strings.ReplaceAll(uuid.New().String(), "-", "")
	}

	reqBody := WebChatRequest{
		MsgID:          strings.ReplaceAll(uuid.New().String(), "-", ""),
		ConversationID: conversationID,
		Query:          query,
		IsEditedQuery:  false,
		ModelConfig: ModelConfig{
			EnableThinking:  thinking,
			WebSearchStatus: "disabled",
			Model:           model,
			Temperature:     0.8,
			TopP:            0.95,
		},
		MultiMedias: []interface{}{},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	// 根据模型选择通道：ultraspeed 走 fastchat，其余走 open-apis
	// （save 路径在 SaveConversation 里按同规则选择）
	chatPath := chatAPI
	if model == modelUltraSpeed {
		chatPath = ultraChatAPI
	}
	reqURL := fmt.Sprintf("%s%s?xiaomichatbot_ph=%s", webBaseURL, chatPath, url.QueryEscape(c.ph))
	httpReq, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	setBrowserHeaders(httpReq.Header)
	httpReq.Header.Set("Cookie", c.buildCookie())

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("mimo returned %d: %s", resp.StatusCode, string(errBody))
	}

	return resp.Body, nil
}

// WebSSEEvent 是网页端 SSE 事件
type WebSSEEvent struct {
	ID    string
	Event string
	Data  string
}

// ParseWebSSE 解析网页端 SSE 流
func ParseWebSSE(ctx context.Context, reader io.ReadCloser, events chan<- WebSSEEvent) error {
	defer reader.Close()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var event WebSSEEvent
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()
		if line == "" {
			if event.Event != "" || event.Data != "" {
				events <- event
				event = WebSSEEvent{}
			}
			continue
		}

		if strings.HasPrefix(line, "id:") {
			event.ID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		} else if strings.HasPrefix(line, "event:") {
			event.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			event.Data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
	}
	return scanner.Err()
}

// SaveConversation 保存对话到 MiMo 官网（维持服务端上下文的关键）
// ultra=true 时保存到 fastchat 通道（与该对话的 chat 通道保持一致）
func (c *WebClient) SaveConversation(ctx context.Context, conversationID, query string, ultra bool) {
	encodedPh := url.QueryEscape(c.ph)
	saveAPI := "/open-apis/chat/conversation/save"
	if ultra {
		saveAPI = ultraSaveAPI
	}
	saveURL := fmt.Sprintf("%s%s?xiaomichatbot_ph=%s", webBaseURL, saveAPI, encodedPh)

	title := query
	if len(title) > 30 {
		title = title[:30]
	}
	savePayload := map[string]interface{}{
		"conversationId": conversationID,
		"title":          title,
		"type":           "chat",
		"multiMedias":    []interface{}{},
	}
	body, _ := json.Marshal(savePayload)

	req, err := http.NewRequestWithContext(ctx, "POST", saveURL, bytes.NewReader(body))
	if err != nil {
		log.Printf("[save] create request error: %v", err)
		return
	}
	setBrowserHeaders(req.Header)
	req.Header.Set("Cookie", c.buildCookie())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Printf("[save] request error: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("[save] conv=%s status=%d body=%s", conversationID, resp.StatusCode, string(respBody))
	}
}

// Validate 验证 Cookie 是否有效
// 2026-09 实测：/open-apis/user/info 已下线（404），换用浏览器真实调用的
// /open-apis/user/mi/get（与 ccp-p/mimo2api 等活跃项目一致）
func (c *WebClient) Validate(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", webBaseURL+"/open-apis/user/mi/get", nil)
	if err != nil {
		return err
	}
	setBrowserHeaders(req.Header)
	req.Header.Set("Cookie", c.buildCookie())
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("invalid: status %d", resp.StatusCode)
	}
	// 上游 200 时 body 里仍可能带业务错误码，进一步校验 code==0
	var result struct {
		Code int `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err == nil && result.Code != 0 {
		return fmt.Errorf("invalid: code %d", result.Code)
	}
	return nil
}
