package stats

import (
	"encoding/json"
	"os"
	"sort"
	"sync"
	"time"
)

// UsageRecord 单次请求的 token 用量
type UsageRecord struct {
	Timestamp        time.Time `json:"timestamp"`
	Model            string    `json:"model"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	CachedTokens     int       `json:"cached_tokens"`
	ReasoningTokens  int       `json:"reasoning_tokens"`
	TotalTokens      int       `json:"total_tokens"`
}

// DailyStats 每日统计
type DailyStats struct {
	Date             string `json:"date"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	CachedTokens     int    `json:"cached_tokens"`
	ReasoningTokens  int    `json:"reasoning_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	RequestCount     int    `json:"request_count"`
}

// ModelStats 按模型统计
type ModelStats struct {
	Model            string `json:"model"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	CachedTokens     int    `json:"cached_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	RequestCount     int    `json:"request_count"`
}

// StatsResponse 完整统计响应
type StatsResponse struct {
	Total       TotalStats     `json:"total"`
	ByDay       []DailyStats   `json:"by_day"`
	ByModel     []ModelStats   `json:"by_model"`
	Concurrency int            `json:"concurrency"`
	Recent      []UsageRecord  `json:"recent"`
}

// TotalStats 总量统计
type TotalStats struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	CachedTokens     int `json:"cached_tokens"`
	ReasoningTokens  int `json:"reasoning_tokens"`
	TotalTokens      int `json:"total_tokens"`
	RequestCount     int `json:"request_count"`
	CacheHitRate     float64 `json:"cache_hit_rate"`
}

// Tracker 追踪器
type Tracker struct {
	mu       sync.RWMutex
	records  []UsageRecord
	path     string
	concurrency int32 // 原子操作，但用 mutex 也行
	convMu      sync.RWMutex
}

var globalTracker *Tracker

// Init 初始化追踪器
func Init(dataPath string) {
	globalTracker = &Tracker{
		path: dataPath,
	}
	globalTracker.load()
}

// Get 获取全局追踪器
func Get() *Tracker {
	return globalTracker
}

// Record 记录一次请求的 token 用量
func (t *Tracker) Record(model string, prompt, completion, cached, reasoning, total int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	rec := UsageRecord{
		Timestamp:        time.Now(),
		Model:            model,
		PromptTokens:     prompt,
		CompletionTokens: completion,
		CachedTokens:     cached,
		ReasoningTokens:  reasoning,
		TotalTokens:      total,
	}
	t.records = append(t.records, rec)
	t.save()
}

// IncrConcurrency 增加并发数
func (t *Tracker) IncrConcurrency() {
	t.convMu.Lock()
	t.concurrency++
	t.convMu.Unlock()
}

// DecrConcurrency 减少并发数
func (t *Tracker) DecrConcurrency() {
	t.convMu.Lock()
	if t.concurrency > 0 {
		t.concurrency--
	}
	t.convMu.Unlock()
}

// GetConcurrency 获取当前并发数
func (t *Tracker) GetConcurrency() int {
	t.convMu.RLock()
	defer t.convMu.RUnlock()
	return int(t.concurrency)
}

// GetStats 获取完整统计
func (t *Tracker) GetStats() StatsResponse {
	t.mu.RLock()
	defer t.mu.RUnlock()

	resp := StatsResponse{
		Concurrency: t.GetConcurrency(),
	}

	// 总量
	for _, r := range t.records {
		resp.Total.PromptTokens += r.PromptTokens
		resp.Total.CompletionTokens += r.CompletionTokens
		resp.Total.CachedTokens += r.CachedTokens
		resp.Total.ReasoningTokens += r.ReasoningTokens
		resp.Total.TotalTokens += r.TotalTokens
		resp.Total.RequestCount++
	}
	if resp.Total.PromptTokens > 0 {
		resp.Total.CacheHitRate = float64(resp.Total.CachedTokens) / float64(resp.Total.PromptTokens) * 100
	}

	// 按天
	dayMap := make(map[string]*DailyStats)
	for _, r := range t.records {
		day := r.Timestamp.Format("2006-01-02")
		if _, ok := dayMap[day]; !ok {
			dayMap[day] = &DailyStats{Date: day}
		}
		d := dayMap[day]
		d.PromptTokens += r.PromptTokens
		d.CompletionTokens += r.CompletionTokens
		d.CachedTokens += r.CachedTokens
		d.ReasoningTokens += r.ReasoningTokens
		d.TotalTokens += r.TotalTokens
		d.RequestCount++
	}
	for _, d := range dayMap {
		resp.ByDay = append(resp.ByDay, *d)
	}
	sort.Slice(resp.ByDay, func(i, j int) bool {
		return resp.ByDay[i].Date < resp.ByDay[j].Date
	})

	// 按模型
	modelMap := make(map[string]*ModelStats)
	for _, r := range t.records {
		if _, ok := modelMap[r.Model]; !ok {
			modelMap[r.Model] = &ModelStats{Model: r.Model}
		}
		m := modelMap[r.Model]
		m.PromptTokens += r.PromptTokens
		m.CompletionTokens += r.CompletionTokens
		m.CachedTokens += r.CachedTokens
		m.TotalTokens += r.TotalTokens
		m.RequestCount++
	}
	for _, m := range modelMap {
		resp.ByModel = append(resp.ByModel, *m)
	}

	// 最近 50 条
	start := 0
	if len(t.records) > 50 {
		start = len(t.records) - 50
	}
	// 倒序（最新在前）
	recent := make([]UsageRecord, 0, len(t.records)-start)
	for i := len(t.records) - 1; i >= start; i-- {
		recent = append(recent, t.records[i])
	}
	resp.Recent = recent

	return resp
}

func (t *Tracker) save() {
	if t.path == "" {
		return
	}
	data, err := json.Marshal(t.records)
	if err != nil {
		return
	}
	os.WriteFile(t.path, data, 0644)
}

func (t *Tracker) load() {
	if t.path == "" {
		return
	}
	data, err := os.ReadFile(t.path)
	if err != nil {
		return
	}
	json.Unmarshal(data, &t.records)
}
