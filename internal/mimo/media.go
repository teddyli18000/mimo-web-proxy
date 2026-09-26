package mimo

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// 上游多模态资源接口（2026-09 实测有效）：
//
//	POST /open-apis/resource/genUploadInfo  → 取签名上传地址
//	PUT  <签名地址>                          → 传二进制
//	POST /open-apis/resource/parse          → 换取对话要用的资源 id
//
// 拿到 id 后放进 bot/chat 的 multiMedias 数组，模型即可看到图片。
const (
	genUploadInfoAPI = "/open-apis/resource/genUploadInfo"
	parseResourceAPI = "/open-apis/resource/parse"
)

// mediaParseModel 用于资源解析的模型。
// 解析接口不接受 ultraspeed 的模型名（实测 parse 返回非 0），
// 用 flash 解析、用请求指定的模型对话即可，两者互不影响。
const mediaParseModel = "mimo-v2.6-flash"

// mediaCacheTTL 已上传资源的缓存时长：同一张图重复上传既慢又浪费额度
const mediaCacheTTL = 6 * time.Hour

type mediaEntry struct {
	obj       map[string]interface{}
	createdAt time.Time
}

var (
	mediaMu    sync.Mutex
	mediaCache = map[string]*mediaEntry{} // md5 -> 上游资源对象
)

// MediaFromDataURL 把 data:image/png;base64,… 解析为原始字节与 MIME 类型。
// 非 data URL（http(s) 直链）返回 ok=false，调用方按不支持处理。
func MediaFromDataURL(dataURL string) (data []byte, mime string, ok bool) {
	if !strings.HasPrefix(dataURL, "data:") {
		return nil, "", false
	}
	comma := strings.Index(dataURL, ",")
	if comma < 0 {
		return nil, "", false
	}
	meta := dataURL[len("data:"):comma]
	if semi := strings.Index(meta, ";"); semi >= 0 {
		mime = meta[:semi]
	}
	if !strings.Contains(meta, "base64") {
		return nil, "", false
	}
	raw, err := base64.StdEncoding.DecodeString(dataURL[comma+1:])
	if err != nil || len(raw) == 0 {
		return nil, "", false
	}
	if mime == "" {
		mime = "image/png"
	}
	return raw, mime, true
}

// UploadMedia 把一张图片上传到上游并返回 multiMedias 条目。
// 相同内容命中缓存时直接复用（多轮对话里同一张图很常见）。
func (c *WebClient) UploadMedia(ctx context.Context, data []byte, mime string) (map[string]interface{}, error) {
	sum := md5.Sum(data)
	key := hex.EncodeToString(sum[:])

	mediaMu.Lock()
	if e, ok := mediaCache[key]; ok && time.Since(e.createdAt) < mediaCacheTTL {
		mediaMu.Unlock()
		return e.obj, nil
	}
	mediaMu.Unlock()

	ext := ".png"
	switch mime {
	case "image/jpeg", "image/jpg":
		ext = ".jpg"
	case "image/webp":
		ext = ".webp"
	case "image/gif":
		ext = ".gif"
	}
	fileName := strings.ReplaceAll(uuid.New().String(), "-", "") + ext

	// 1. 取签名上传地址
	var info struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			UploadURL   string `json:"uploadUrl"`
			ResourceURL string `json:"resourceUrl"`
			ObjectName  string `json:"objectName"`
		} `json:"data"`
	}
	body, _ := json.Marshal(map[string]string{"fileName": fileName, "fileContentMd5": key})
	reqURL := fmt.Sprintf("%s%s?xiaomichatbot_ph=%s", webBaseURL, genUploadInfoAPI, url.QueryEscape(c.ph))
	if err := c.postJSON(ctx, reqURL, body, &info); err != nil {
		return nil, fmt.Errorf("genUploadInfo: %w", err)
	}
	if info.Code != 0 || info.Data.UploadURL == "" {
		return nil, fmt.Errorf("genUploadInfo: code=%d msg=%s", info.Code, info.Msg)
	}

	// 2. PUT 二进制到签名地址
	putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, info.Data.UploadURL, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	putReq.Header.Set("Content-Type", "application/octet-stream")
	putReq.Header.Set("content-md5", key)
	putResp, err := c.httpClient.Do(putReq)
	if err != nil {
		return nil, fmt.Errorf("upload PUT: %w", err)
	}
	io.Copy(io.Discard, putResp.Body)
	putResp.Body.Close()
	if putResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upload PUT: status %d", putResp.StatusCode)
	}

	// 3. 解析换取资源 id（上游视觉引擎需要一点时间，失败重试）
	var parsed struct {
		Code int `json:"code"`
		Data struct {
			ID         string `json:"id"`
			TokenUsage int    `json:"tokenUsage"`
		} `json:"data"`
	}
	parseURL := fmt.Sprintf("%s%s?fileUrl=%s&objectName=%s&model=%s&xiaomichatbot_ph=%s",
		webBaseURL, parseResourceAPI,
		url.QueryEscape(info.Data.ResourceURL), url.QueryEscape(info.Data.ObjectName),
		mediaParseModel, url.QueryEscape(c.ph))
	for attempt := 0; attempt < 5; attempt++ {
		if err := c.postJSON(ctx, parseURL, []byte("{}"), &parsed); err == nil && parsed.Code == 0 && parsed.Data.ID != "" {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	if parsed.Data.ID == "" {
		return nil, fmt.Errorf("resource parse: no id")
	}

	obj := map[string]interface{}{
		"mediaType":          "image",
		"fileUrl":            info.Data.ResourceURL,
		"compressedVideoUrl": "",
		"audioTrackUrl":      "",
		"name":               fileName,
		"size":               len(data),
		"status":             "completed",
		"objectName":         info.Data.ObjectName,
		"tokenUsage":         parsed.Data.TokenUsage,
		"url":                parsed.Data.ID,
	}

	mediaMu.Lock()
	mediaCache[key] = &mediaEntry{obj: obj, createdAt: time.Now()}
	mediaMu.Unlock()
	return obj, nil
}

// postJSON 发送 JSON POST 并解析响应（复用浏览器头与 Cookie）
func (c *WebClient) postJSON(ctx context.Context, reqURL string, body []byte, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	setBrowserHeaders(req.Header)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", c.buildCookie())
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(raw))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
