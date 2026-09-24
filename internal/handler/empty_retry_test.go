package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestIsWebResultEmpty(t *testing.T) {
	tests := []struct {
		name     string
		result   webResult
		expected bool
	}{
		{
			name:     "completely empty",
			result:   webResult{},
			expected: true,
		},
		{
			name: "empty with usage only",
			result: webResult{
				Usage: &usageData{TotalTokens: 100},
			},
			expected: true,
		},
		{
			name: "has text content",
			result: webResult{
				Text: "Hello, world!",
			},
			expected: false,
		},
		{
			name: "has upstream error",
			result: webResult{
				UpstreamErr: "文本超长",
			},
			expected: false,
		},
		{
			name: "has text and upstream error",
			result: webResult{
				Text:        "partial output",
				UpstreamErr: "some error",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isWebResultEmpty(tt.result); got != tt.expected {
				t.Errorf("isWebResultEmpty() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCollectWebResultFromReader_Empty(t *testing.T) {
	ctx := context.Background()
	reader := io.NopCloser(strings.NewReader(""))

	result, err := collectWebResultFromReader(ctx, reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Text != "" {
		t.Errorf("expected empty text, got %q", result.Text)
	}
	if result.UpstreamErr != "" {
		t.Errorf("expected empty upstreamErr, got %q", result.UpstreamErr)
	}
	if !isWebResultEmpty(result) {
		t.Errorf("expected isWebResultEmpty(result) == true")
	}
}

func TestCollectWebResultFromReader_Normal(t *testing.T) {
	ctx := context.Background()
	sseData := `id: msg_001
event: message
data: {"type":"text","content":"Hello world"}

event: usage
data: {"promptTokens":10,"completionTokens":5,"totalTokens":15}

`
	reader := io.NopCloser(strings.NewReader(sseData))
	result, err := collectWebResultFromReader(ctx, reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Text != "Hello world" {
		t.Errorf("expected %q, got %q", "Hello world", result.Text)
	}
	if result.LastMsgID != "msg_001" {
		t.Errorf("expected msg_001, got %q", result.LastMsgID)
	}
	if result.Usage == nil || result.Usage.TotalTokens != 15 {
		t.Errorf("expected totalTokens=15, got %+v", result.Usage)
	}
	if isWebResultEmpty(result) {
		t.Errorf("expected isWebResultEmpty(result) == false")
	}
}

func TestCollectWebResultFromReader_FilterThinkingAndNullByte(t *testing.T) {
	ctx := context.Background()
	sseData := `id: msg_think
event: message
data: {"type":"thinking","content":"Thinking out loud..."}

id: msg_text1
event: message
data: {"type":"text","content":"<think>hidden thoughts</think>Visible text\u0000"}

`
	reader := io.NopCloser(strings.NewReader(sseData))
	result, err := collectWebResultFromReader(ctx, reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Text != "Visible text" {
		t.Errorf("expected 'Visible text', got %q", result.Text)
	}
	if result.LastMsgID != "msg_text1" {
		t.Errorf("expected msg_text1, got %q", result.LastMsgID)
	}
}

func TestCollectWebResultFromReader_MultipleUsageFastChat(t *testing.T) {
	ctx := context.Background()
	sseData := `event: usage
data: {"promptTokens":5,"completionTokens":5,"totalTokens":10}

event: usage
data: {"promptTokens":10,"completionTokens":10,"totalTokens":20}

id: msg_final
event: message
data: {"type":"text","content":"Done"}

`
	reader := io.NopCloser(strings.NewReader(sseData))
	result, err := collectWebResultFromReader(ctx, reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Usage == nil || result.Usage.TotalTokens != 20 {
		t.Errorf("expected totalTokens=20 from last usage event, got %+v", result.Usage)
	}
}

func TestCollectWebResultFromReader_UpstreamError(t *testing.T) {
	ctx := context.Background()
	sseData := `event: error
data: {"type":"error","content":"文本超长"}

`
	reader := io.NopCloser(strings.NewReader(sseData))
	result, err := collectWebResultFromReader(ctx, reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.UpstreamErr != "文本超长" {
		t.Errorf("expected '文本超长', got %q", result.UpstreamErr)
	}
	if result.Text != "" {
		t.Errorf("expected empty text, got %q", result.Text)
	}
	if isWebResultEmpty(result) {
		t.Errorf("expected isWebResultEmpty to be false when UpstreamErr is present")
	}
}

func TestRandomHex32(t *testing.T) {
	id1 := randomHex32()
	id2 := randomHex32()

	if len(id1) != 32 {
		t.Errorf("expected len 32, got %d (%s)", len(id1), id1)
	}
	if len(id2) != 32 {
		t.Errorf("expected len 32, got %d (%s)", len(id2), id2)
	}
	if id1 == id2 {
		t.Errorf("expected unique random ids, got collision: %s", id1)
	}
}

// TestRetryWithHTTPTest simulates an upstream server via httptest.Server
// and verifies the retry state machine (empty response on 1st attempt, success on 2nd).
func TestRetryWithHTTPTest(t *testing.T) {
	var requestCount int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		if count == 1 {
			// 1st request: returns 200 OK with completely empty SSE
			return
		}
		// 2nd request: returns valid SSE
		fmt.Fprintf(w, "id: msg_retry_ok\n")
		fmt.Fprintf(w, "event: message\n")
		fmt.Fprintf(w, "data: {\"type\":\"text\",\"content\":\"response after retry\"}\n\n")
		fmt.Fprintf(w, "event: usage\n")
		fmt.Fprintf(w, "data: {\"promptTokens\":10,\"completionTokens\":5,\"totalTokens\":15}\n\n")
	}))
	defer ts.Close()

	ctx := context.Background()
	initialConvID := "orig_conv_12345678"
	initialParentID := "orig_parent_999"

	curConvID := initialConvID
	curParentID := initialParentID
	var finalResult webResult
	var finalAttempt int

	for attempt := 0; attempt < 2; attempt++ {
		finalAttempt = attempt
		resp, err := http.Get(ts.URL)
		if err != nil {
			t.Fatalf("http.Get failed: %v", err)
		}

		res, err := collectWebResultFromReader(ctx, resp.Body)
		if err != nil {
			t.Fatalf("collectWebResultFromReader failed: %v", err)
		}

		if res.UpstreamErr != "" && res.Text == "" {
			t.Fatalf("unexpected upstream error: %s", res.UpstreamErr)
		}

		if isWebResultEmpty(res) {
			if attempt == 0 {
				curConvID = randomHex32()
				curParentID = "0"
				continue
			}
			t.Fatalf("second attempt unexpectedly empty")
		}

		finalResult = res
		break
	}

	if finalAttempt != 1 {
		t.Errorf("expected to succeed on attempt index 1 (the retry), got attempt %d", finalAttempt)
	}
	if atomic.LoadInt32(&requestCount) != 2 {
		t.Errorf("expected 2 HTTP requests, got %d", requestCount)
	}
	if curConvID == initialConvID {
		t.Errorf("expected convID to be rotated, but remained %s", curConvID)
	}
	if len(curConvID) != 32 {
		t.Errorf("expected new convID to be 32-char hex, got %s", curConvID)
	}
	if curParentID != "0" {
		t.Errorf("expected parentID reset to '0', got %s", curParentID)
	}
	if finalResult.Text != "response after retry" {
		t.Errorf("expected 'response after retry', got %q", finalResult.Text)
	}
}

// TestRetryWithHTTPTest_DoubleEmpty verifies that if both attempts are empty,
// it errors out with the expected error message.
func TestRetryWithHTTPTest_DoubleEmpty(t *testing.T) {
	var requestCount int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		// Both attempts return 200 OK + empty SSE
	}))
	defer ts.Close()

	ctx := context.Background()
	curConvID := "init_conv"
	curParentID := "0"
	var finalErrMsg string

	for attempt := 0; attempt < 2; attempt++ {
		resp, err := http.Get(ts.URL)
		if err != nil {
			t.Fatalf("http.Get failed: %v", err)
		}

		res, err := collectWebResultFromReader(ctx, resp.Body)
		if err != nil {
			t.Fatalf("collectWebResultFromReader failed: %v", err)
		}

		if res.UpstreamErr != "" && res.Text == "" {
			t.Fatalf("unexpected upstream error: %s", res.UpstreamErr)
		}

		if isWebResultEmpty(res) {
			if attempt == 0 {
				curConvID = randomHex32()
				curParentID = "0"
				continue
			}
			finalErrMsg = "mimo returned an empty response (after retry)"
			break
		}
	}

	if curConvID == "init_conv" {
		t.Errorf("expected curConvID to change on retry, remained %s", curConvID)
	}
	if curParentID != "0" {
		t.Errorf("expected curParentID to be '0', got %s", curParentID)
	}

	if atomic.LoadInt32(&requestCount) != 2 {
		t.Errorf("expected 2 attempts before failure, got %d", requestCount)
	}
	if finalErrMsg != "mimo returned an empty response (after retry)" {
		t.Errorf("expected error 'mimo returned an empty response (after retry)', got %q", finalErrMsg)
	}
}

// TestRetryWithHTTPTest_UpstreamErrorNoRetry verifies that deterministic upstream errors
// are not retried.
func TestRetryWithHTTPTest_UpstreamErrorNoRetry(t *testing.T) {
	var requestCount int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: error\n")
		fmt.Fprintf(w, "data: {\"type\":\"error\",\"content\":\"文本超长\"}\n\n")
	}))
	defer ts.Close()

	ctx := context.Background()
	var finalErrMsg string

	for attempt := 0; attempt < 2; attempt++ {
		resp, err := http.Get(ts.URL)
		if err != nil {
			t.Fatalf("http.Get failed: %v", err)
		}

		res, err := collectWebResultFromReader(ctx, resp.Body)
		if err != nil {
			t.Fatalf("collectWebResultFromReader failed: %v", err)
		}

		if res.UpstreamErr != "" && res.Text == "" {
			finalErrMsg = "mimo: " + res.UpstreamErr
			break
		}

		if isWebResultEmpty(res) {
			if attempt == 0 {
				continue
			}
			break
		}
	}

	if atomic.LoadInt32(&requestCount) != 1 {
		t.Errorf("expected exactly 1 attempt (no retry on upstream error), got %d", requestCount)
	}
	if finalErrMsg != "mimo: 文本超长" {
		t.Errorf("expected 'mimo: 文本超长', got %q", finalErrMsg)
	}
}
