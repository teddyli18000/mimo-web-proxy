package adapter

import (
	"fmt"
	"strings"
)

// NormalizeContent 把消息 content 归一成纯文本。
//
// 兼容 OpenAI 与 Anthropic 两种内容块形态：
//   - 字符串 → 原样返回
//   - 块数组 → 取 text/output_text/input_text 的文本，tool_result 取其内容
//   - 其他类型 → 退化为 %v
//
// 注意：tool_use 块会被跳过——工具调用由 handler 侧单独渲染（带参数），
// 在这里重复拼接会导致历史里出现两份调用记录。
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
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			switch m["type"] {
			case "text", "output_text", "input_text":
				if text, ok := m["text"].(string); ok && text != "" {
					parts = append(parts, text)
				}
			case "tool_result":
				if content, ok := m["content"].(string); ok && content != "" {
					parts = append(parts, content)
					continue
				}
				if contentArr, ok := m["content"].([]interface{}); ok {
					for _, c := range contentArr {
						if cm, ok := c.(map[string]interface{}); ok {
							if text, ok := cm["text"].(string); ok && text != "" {
								parts = append(parts, text)
							}
						}
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return fmt.Sprintf("%v", v)
	}
}
