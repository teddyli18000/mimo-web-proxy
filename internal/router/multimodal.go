package router

import (
	"strings"

	"github.com/teddyli18000/mimo-web-proxy/internal/mimo"
)

// 当前上游（aistudio.xiaomimimo.com/open-apis/bot/config）提供的模型，
// 2026-09 实测。v2.6 全系列支持多模态与思考模式。
const (
	ModelV26Flash      = "mimo-v2.6-flash"
	ModelV26Pro        = "mimo-v2.6-pro"
	ModelV26UltraSpeed = "mimo-v2.6-pro-ultraspeed-studio"
	ModelV25           = "mimo-v2.5"
	ModelV25Pro        = "mimo-v2.5-pro"
)

// DefaultModel 纯文本默认模型
const DefaultModel = ModelV26Pro

// DefaultMultimodalModel 含图片/音频/文件时的默认模型
const DefaultMultimodalModel = ModelV26Flash

// RouteResult 路由决策结果
type RouteResult struct {
	Model  string // 实际要使用的模型
	Reason string // 路由原因
}

// RouteModel 根据消息内容决定使用哪个模型
// 规则：
// 1. 用户显式指定模型 → 尊重
// 2. 未指定（空/auto）→ 含多模态内容用 v2.6-flash，纯文本用 v2.6-pro
func RouteModel(requestedModel string, messages []mimo.Message) RouteResult {
	if requestedModel != "" && requestedModel != "auto" {
		normalized := normalizeModel(requestedModel)
		return RouteResult{Model: normalized, Reason: "explicit"}
	}

	for _, msg := range messages {
		if hasMultimodalContent(msg.Content) {
			return RouteResult{Model: DefaultMultimodalModel, Reason: "multimodal_content"}
		}
	}

	return RouteResult{Model: DefaultModel, Reason: "text_only"}
}

// hasMultimodalContent 检查内容是否包含非文本部分
func hasMultimodalContent(content interface{}) bool {
	switch v := content.(type) {
	case string:
		return false
	case []interface{}:
		for _, part := range v {
			if m, ok := part.(map[string]interface{}); ok {
				t, _ := m["type"].(string)
				if t == "image_url" || t == "audio" || t == "file" || t == "image" {
					return true
				}
			}
		}
	}
	return false
}

// normalizeModel 标准化模型名称（含历史别名兼容）
func normalizeModel(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	switch m {
	case "mimo-v2.6-flash", "mimo-v2.6-flash-studio":
		return ModelV26Flash
	case "mimo-v2.6-pro", "mimo-v2.6-pro-studio":
		return ModelV26Pro
	case "mimo-v2.6-pro-ultraspeed-studio", "mimo-v2.6-ultraspeed", "mimo-v2.6-pro-ultraspeed":
		return ModelV26UltraSpeed
	case "mimo-v2.5", "mimo-v2.5-omni", "mimo-v2-omni":
		return ModelV25
	case "mimo-v2.5-pro", "mimo-v2-pro":
		return ModelV25Pro
	default:
		// 含已知关键词的模糊匹配
		if strings.Contains(m, "ultraspeed") {
			return ModelV26UltraSpeed
		}
		if strings.Contains(m, "2.6") {
			if strings.Contains(m, "flash") {
				return ModelV26Flash
			}
			return ModelV26Pro
		}
		if strings.Contains(m, "omni") || (strings.Contains(m, "2.5") && !strings.Contains(m, "pro")) {
			return ModelV25
		}
		if strings.Contains(m, "2.5") && strings.Contains(m, "pro") {
			return ModelV25Pro
		}
		if strings.Contains(m, "pro") {
			return ModelV26Pro
		}
		return DefaultModel
	}
}

// SupportedModels 返回支持的模型列表（OpenAI /v1/models 格式）
func SupportedModels() []map[string]interface{} {
	fullCaps := func() map[string]interface{} {
		return map[string]interface{}{
			"text": true, "image": true, "audio": true, "file": true, "reasoning": true,
		}
	}
	return []map[string]interface{}{
		{
			"id":           ModelV26Flash,
			"object":       "model",
			"owned_by":     "xiaomi",
			"capabilities": fullCaps(),
		},
		{
			"id":           ModelV26Pro,
			"object":       "model",
			"owned_by":     "xiaomi",
			"capabilities": fullCaps(),
		},
		{
			"id":           ModelV26UltraSpeed,
			"object":       "model",
			"owned_by":     "xiaomi",
			"capabilities": fullCaps(),
		},
		{
			"id":           ModelV25,
			"object":       "model",
			"owned_by":     "xiaomi",
			"capabilities": fullCaps(),
		},
		{
			"id":       ModelV25Pro,
			"object":   "model",
			"owned_by": "xiaomi",
			"capabilities": map[string]interface{}{
				"text": true, "image": true, "file": true, "reasoning": true,
			},
		},
	}
}

// MaxQueryCharsForModel 返回该模型通道的单次 query 字符上限。
// fastchat 通道（ultraspeed）实测约 48.7K 就拒绝，open-apis 约 100K。
func MaxQueryCharsForModel(model string) int {
	if model == ModelV26UltraSpeed {
		return 45000 // fastchat 实测 48750 OK / 50000 拒，留余量
	}
	return 90000 // open-apis 实测 ~100K 拒，留余量
}
