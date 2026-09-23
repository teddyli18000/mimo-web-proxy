package stats

import (
	"encoding/json"
	"os"
	"sync"
)

// ConvState 对话状态，包含 MiMo 对话 ID 和最后一条 AI 消息 ID
type ConvState struct {
	MimoConvID    string `json:"mimoConvID"`
	ParentID      string `json:"parentID"`
	OriginalQuery string `json:"originalQuery,omitempty"`
}

// ConversationStore 对话 ID 映射存储
type ConversationStore struct {
	mu    sync.RWMutex
	// clientConvID -> ConvState
	store map[string]*ConvState
	path  string
}

var globalConvStore *ConversationStore

// InitConvStore 初始化对话存储
func InitConvStore(dataPath string) {
	globalConvStore = &ConversationStore{
		store: make(map[string]*ConvState),
		path:  dataPath,
	}
	globalConvStore.load()
}

// GetConvStore 获取全局对话存储
func GetConvStore() *ConversationStore {
	return globalConvStore
}

// GetMimoConvID 获取 MiMo 对话 ID
func (s *ConversationStore) GetMimoConvID(clientConvID string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.store[clientConvID]
	if !ok {
		return "", false
	}
	return st.MimoConvID, true
}

// GetParentID 获取对话的最后一条 AI 消息 ID
func (s *ConversationStore) GetParentID(clientConvID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.store[clientConvID]
	if !ok {
		return ""
	}
	return st.ParentID
}

// SetMimoConvID 设置 MiMo 对话 ID
func (s *ConversationStore) SetMimoConvID(clientConvID, mimoConvID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.store[clientConvID]
	if !ok {
		st = &ConvState{}
		s.store[clientConvID] = st
	}
	st.MimoConvID = mimoConvID
	s.save()
}

// SetParentID 设置对话的最后一条 AI 消息 ID
func (s *ConversationStore) SetParentID(clientConvID, parentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.store[clientConvID]
	if !ok {
		st = &ConvState{}
		s.store[clientConvID] = st
	}
	st.ParentID = parentID
	s.save()
}

// GetOriginalQuery 获取对话的原始用户问题
func (s *ConversationStore) GetOriginalQuery(clientConvID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.store[clientConvID]
	if !ok {
		return ""
	}
	return st.OriginalQuery
}

// SetOriginalQuery 设置对话的原始用户问题
func (s *ConversationStore) SetOriginalQuery(clientConvID, query string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.store[clientConvID]
	if !ok {
		st = &ConvState{}
		s.store[clientConvID] = st
	}
	st.OriginalQuery = query
	s.save()
}

func (s *ConversationStore) save() {
	if s.path == "" {
		return
	}
	data, err := json.MarshalIndent(s.store, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(s.path, data, 0644)
}

func (s *ConversationStore) load() {
	if s.path == "" {
		return
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	// 兼容旧格式 map[string]string
	var oldFormat map[string]string
	if err := json.Unmarshal(data, &oldFormat); err == nil {
		// 迁移旧格式
		for k, v := range oldFormat {
			s.store[k] = &ConvState{MimoConvID: v}
		}
		return
	}
	json.Unmarshal(data, &s.store)
}
