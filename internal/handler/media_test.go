package handler

import (
	"strings"
	"testing"

	"github.com/teddyli18000/mimo-web-proxy/internal/adapter"
	"github.com/teddyli18000/mimo-web-proxy/internal/mimo"
)

func TestCollectImagesOpenAI(t *testing.T) {
	msgs := []adapter.OpenAIMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: []interface{}{
			map[string]interface{}{"type": "text", "text": "这是什么颜色"},
			map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/png;base64,AAA"}},
			map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/jpeg;base64,BBB"}},
		}},
		{Role: "user", Content: "纯文本"},
	}
	got := collectImagesOpenAI(msgs)
	if len(got) != 2 {
		t.Fatalf("期望 2 张图，实际 %d", len(got))
	}
	if got[0] != "data:image/png;base64,AAA" || got[1] != "data:image/jpeg;base64,BBB" {
		t.Errorf("提取结果不符: %v", got)
	}
	// image_url 直接是字符串的形态
	alt := []adapter.OpenAIMessage{{Role: "user", Content: []interface{}{
		map[string]interface{}{"type": "image_url", "image_url": "data:image/png;base64,CCC"},
	}}}
	if g := collectImagesOpenAI(alt); len(g) != 1 || g[0] != "data:image/png;base64,CCC" {
		t.Errorf("字符串形态未提取: %v", g)
	}
	// 没有图片时返回空
	if g := collectImagesOpenAI([]adapter.OpenAIMessage{{Role: "user", Content: "hi"}}); len(g) != 0 {
		t.Errorf("纯文本不应提取出图片: %v", g)
	}
}

func TestCollectImagesAnthropic(t *testing.T) {
	msgs := []adapter.AnthropicMessage{
		{Role: "user", Content: []interface{}{
			map[string]interface{}{"type": "image", "source": map[string]interface{}{
				"type": "base64", "media_type": "image/png", "data": "ZZZ",
			}},
			map[string]interface{}{"type": "text", "text": "什么颜色"},
		}},
	}
	got := collectImagesAnthropic(msgs)
	if len(got) != 1 {
		t.Fatalf("期望 1 张图，实际 %d", len(got))
	}
	if got[0] != "data:image/png;base64,ZZZ" {
		t.Errorf("未转成 data URL: %v", got[0])
	}
	// 缺 media_type 时退回 png
	msgs[0].Content = []interface{}{map[string]interface{}{"type": "image", "source": map[string]interface{}{"type": "base64", "data": "YYY"}}}
	if g := collectImagesAnthropic(msgs); len(g) != 1 || !strings.HasPrefix(g[0], "data:image/png;base64,") {
		t.Errorf("缺 media_type 未兜底: %v", g)
	}
	// tool_result 里不含图片，不应误取
	toolOnly := []adapter.AnthropicMessage{{Role: "user", Content: []interface{}{
		map[string]interface{}{"type": "tool_result", "content": "x"},
	}}}
	if g := collectImagesAnthropic(toolOnly); len(g) != 0 {
		t.Errorf("tool_result 不应提取图片: %v", g)
	}
}

func TestMediaFromDataURL(t *testing.T) {
	cases := []struct {
		in      string
		ok      bool
		mime    string
		wantLen int
	}{
		{"data:image/png;base64,aGVsbG8=", true, "image/png", 5},
		{"data:image/jpeg;base64,aGVsbG8=", true, "image/jpeg", 5},
		{"data:image/webp;base64,aGVsbG8=", true, "image/webp", 5},
		{"https://example.com/a.png", false, "", 0},
		{"data:image/png,notbase64", false, "", 0},
		{"data:image/png;base64,!!!invalid!!!", false, "", 0},
		{"", false, "", 0},
	}
	for _, c := range cases {
		data, mime, ok := mimo.MediaFromDataURL(c.in)
		if ok != c.ok {
			t.Errorf("MediaFromDataURL(%q) ok=%v, want %v", c.in, ok, c.ok)
			continue
		}
		if ok && (mime != c.mime || len(data) != c.wantLen) {
			t.Errorf("MediaFromDataURL(%q) = (%d 字节, %s), want (%d, %s)", c.in, len(data), mime, c.wantLen, c.mime)
		}
	}
}
