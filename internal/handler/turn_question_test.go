package handler

import (
	"strings"
	"testing"

	"github.com/teddyli18000/mimo-web-proxy/internal/adapter"
)

func msg(role, text string) adapter.OpenAIMessage {
	return adapter.OpenAIMessage{Role: role, Content: text}
}

func TestCurrentTurnQuestion(t *testing.T) {
	cases := []struct {
		name string
		msgs []adapter.OpenAIMessage
		want string
	}{
		{
			name: "首轮：system + 提问",
			msgs: []adapter.OpenAIMessage{msg("system", "sys"), msg("user", "帮我查日志")},
			want: "帮我查日志",
		},
		{
			name: "首轮：提问在前、注入在后（DSH 形态）",
			msgs: []adapter.OpenAIMessage{
				msg("system", "sys"),
				msg("user", "插件市场卡住了"),
				msg("user", "<system-reminder>AGENTS.md…</system-reminder>"),
				msg("user", "Current runtime context. workspace-write"),
			},
			want: "插件市场卡住了",
		},
		{
			name: "工具结果轮：没有新提问",
			msgs: []adapter.OpenAIMessage{
				msg("system", "sys"),
				msg("user", "插件市场卡住了"),
				msg("assistant", "[called pwsh]"),
				msg("tool", "命令输出"),
			},
			want: "",
		},
		{
			name: "第二轮：assistant 之后的新提问",
			msgs: []adapter.OpenAIMessage{
				msg("system", "sys"),
				msg("user", "第一个问题"),
				msg("assistant", "回答"),
				msg("user", "第二个问题"),
			},
			want: "第二个问题",
		},
		{
			name: "本轮以注入开头（没有真实提问）",
			msgs: []adapter.OpenAIMessage{
				msg("system", "sys"),
				msg("assistant", "回答"),
				msg("user", "<system-reminder>只有注入</system-reminder>"),
			},
			want: "",
		},
		{
			name: "空列表",
			msgs: nil,
			want: "",
		},
		{
			name: "只有 system",
			msgs: []adapter.OpenAIMessage{msg("system", "sys")},
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := currentTurnQuestion(c.msgs); got != c.want {
				t.Errorf("currentTurnQuestion() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestBuildToolPromptCarriesParamSpec(t *testing.T) {
	tools := []adapter.OpenAITool{{
		Type: "function",
		Function: adapter.OpenAIToolFunction{
			Name:        "pwsh",
			Description: "Run a PowerShell command.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command":     map[string]interface{}{"type": "string"},
					"description": map[string]interface{}{"type": "string"},
					"timeout":     map[string]interface{}{"type": "number"},
				},
				"required": []interface{}{"command", "description"},
			},
		},
	}}
	got := buildToolPrompt(tools)

	// 必填参数必须标注，否则模型会漏传（2026-09 实测 missing required property）
	for _, want := range []string{
		"command: string (required)",
		"description: string (required)",
		"timeout: number",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("工具提示缺少 %q\n实际内容:\n%s", want, got)
		}
	}
	if strings.Contains(got, "timeout: number (required)") {
		t.Error("可选参数被误标为 required")
	}
	// 调用格式说明仍需保留
	if !strings.Contains(got, "<tool_calls>") || !strings.Contains(got, "<invoke name=") {
		t.Error("工具调用格式说明丢失")
	}
}

func TestLooksLikeInjectedContext(t *testing.T) {
	injected := []string{
		"<system-reminder>\nThe following workspace instructions…",
		"Current runtime context. This snapshot supersedes…",
		"Current DSH file policy: workspace-write.",
	}
	for _, s := range injected {
		if !looksLikeInjectedContext(s) {
			t.Errorf("应判定为注入上下文: %q", s)
		}
	}
	real := []string{
		"帮我看看插件市场为什么卡住",
		"我的dsh的插件市场没法更新",
		"<div>这不是 reminder</div>",
	}
	for _, s := range real {
		if looksLikeInjectedContext(s) {
			t.Errorf("不应判定为注入上下文: %q", s)
		}
	}
}
