package convstore

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Store manages conversationId + fingerprint mappings for MiMo conversation reuse.
//
// 会话连续性判断（参考 meny2333/mimo2api_go 的 fingerprint 设计）：
// 客户端（agent 类）每次发来【完整历史】，网关对「最近 5 条非系统消息」取指纹。
// 若新指纹与某会话记录的上一轮指纹呈「前缀延续」关系（客户端在旧历史上追加了新消息），
// 则复用该会话的 conversationId（MiMo 服务端已有上下文，省 token）；
// 否则新建会话（全新 conversationId，服务端上下文从零开始）。
type Store struct {
	mu    sync.RWMutex
	convs map[string]*convState
	path  string // 可选持久化路径（空 = 仅内存）
}

type convState struct {
	ConvID      string // 32-hex conversationId sent to MiMo
	ParentID    string // last AI response message ID from MiMo SSE
	Fingerprint string // fingerprint of last client message list
	MsgCount    int    // 上一轮客户端非 system 消息数（用于判断"是否增长"）
	UpdatedAt   int64  // unix seconds, 用于 LRU 淘汰
}

func New() *Store {
	return &Store{
		convs: make(map[string]*convState),
	}
}

// NewPersisted 创建带 JSON 持久化的 Store（重启后会话不丢，MiMo 服务端上下文仍有效）
func NewPersisted(path string) *Store {
	s := &Store{
		convs: make(map[string]*convState),
		path:  path,
	}
	s.load()
	return s
}

func (s *Store) load() {
	if s.path == "" {
		return
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var saved map[string]*convState
	if json.Unmarshal(data, &saved) == nil && saved != nil {
		s.convs = saved
	}
}

func (s *Store) saveLocked() {
	if s.path == "" {
		return
	}
	// 只保留最近 50 个会话，防文件无限膨胀
	if len(s.convs) > 50 {
		var oldestKey string
		var oldest int64 = 1<<62
		for k, v := range s.convs {
			if v.UpdatedAt < oldest {
				oldest = v.UpdatedAt
				oldestKey = k
			}
		}
		delete(s.convs, oldestKey)
	}
	data, err := json.MarshalIndent(s.convs, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(s.path), 0755)
	_ = os.WriteFile(s.path, data, 0600)
}

// Fingerprint computes the continuity fingerprint of a client message list:
// SHA-256 of the last N (≤5) non-system, non-tool messages (role + content, ≤200 chars each).
// messages 传入 [][2]string 形式的 (role, textContent) 序列。
func Fingerprint(msgs [][2]string) string {
	var recent [][2]string
	for _, m := range msgs {
		role, content := m[0], m[1]
		if role == "system" || role == "tool" {
			continue
		}
		if len(content) > 200 {
			content = truncateUTF8(content, 200)
		}
		recent = append(recent, [2]string{role, content})
	}
	if len(recent) == 0 {
		return ""
	}
	if len(recent) > 5 {
		recent = recent[len(recent)-5:]
	}
	b, _ := json.Marshal(recent)
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h[:16])
}

// Continuation reports whether the client's current message list is a
// prefix-continuation of the previous turn (i.e. the client appended new
// messages on top of the old history — the normal agent loop pattern).
// msgs: full (role, text) list of the CURRENT request; lastFp: stored fingerprint.
func Continuation(msgs [][2]string, lastFp string) bool {
	if lastFp == "" {
		return false
	}
	// 过滤出非系统消息（与 Fingerprint 口径一致）
	var non [][2]string
	for _, m := range msgs {
		if m[0] == "system" || m[0] == "tool" {
			continue
		}
		non = append(non, m)
	}
	if len(non) < 2 {
		return false
	}
	// 客户端历史被截断/污染（旧轮次丢了）时不算延续：
	// 只有「当前序列去掉尾部 1..k 条后某前缀的指纹 == lastFp」才延续。
	// 限制 k ≤ 5（agent 一步通常只追加几条）。
	for drop := 1; drop <= 5 && drop <= len(non); drop++ {
		prefix := non[:len(non)-drop]
		if Fingerprint(prefix) == lastFp {
			return true
		}
	}
	return false
}

// Resolve 决定复用还是新建会话，并返回 (convID, parentID, isNew)。
// msgs: 当前请求的 (role, textContent) 全序列。
func (s *Store) Resolve(msgs [][2]string) (convID, parentID string, isNew bool) {
	fp := Fingerprint(msgs)
	s.mu.Lock()
	defer s.mu.Unlock()

	if fp != "" {
		// 按最近使用时间倒序取第一个匹配：客户端重跑同一段内容时（测试重放、
		// agent 重试、多个客户端用同样的系统提示），会存在多个指纹相同的会话，
		// map 遍历顺序随机 → 可能复用到已推进或已过期的旧会话，导致模型丢上下文
		// （2026-09 实测：多轮测试偶发答非所问）。必须命中最近那个。
		//
		// 同时要求消息数增长：客户端把同一份历史再发一次（重放）不算延续，
		// 否则会把增量发给已经推进过的旧会话，模型只能看到半截对话。
		non := countNonSystem(msgs)
		var best *convState
		for _, cs := range s.convs {
			if cs.Fingerprint == "" || !Continuation(msgs, cs.Fingerprint) {
				continue
			}
			if non <= cs.MsgCount {
				continue
			}
			if best == nil || cs.UpdatedAt > best.UpdatedAt {
				best = cs
			}
		}
		if best != nil {
			best.Fingerprint = fp
			best.MsgCount = non
			best.UpdatedAt = nowUnix()
			s.saveLocked()
			return best.ConvID, best.ParentID, false
		}
	}

	// 新会话
	convID = randomHex32()
	s.convs[convID] = &convState{
		ConvID:      convID,
		ParentID:    "0",
		Fingerprint: fp,
		MsgCount:    countNonSystem(msgs),
		UpdatedAt:   nowUnix(),
	}
	s.saveLocked()
	return convID, "0", true
}

// countNonSystem 统计非 system/tool 消息数（与 Fingerprint 口径一致）
func countNonSystem(msgs [][2]string) int {
	n := 0
	for _, m := range msgs {
		if m[0] != "system" && m[0] != "tool" {
			n++
		}
	}
	return n
}

// SetParentID 更新指定会话的最后 AI 消息 ID
func (s *Store) SetParentID(convID, parentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cs, ok := s.convs[convID]; ok {
		cs.ParentID = parentID
		cs.UpdatedAt = nowUnix()
		s.saveLocked()
	}
}

// ParentID 读取会话的最后 AI 消息 ID
func (s *Store) ParentID(convID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if cs, ok := s.convs[convID]; ok {
		return cs.ParentID
	}
	return ""
}

func truncateUTF8(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

func nowUnix() int64 {
	return time.Now().Unix()
}

// randomHex32 generates a random 32-char hex string using crypto/rand.
func randomHex32() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
