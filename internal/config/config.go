package config

import (
	"encoding/json"
	"os"
	"sync"
)

type Config struct {
	Port         string    `json:"port"`
	APIKey       string    `json:"api_key"` // 网关自身的认证 Key
	DefaultModel string    `json:"default_model"`
	Accounts     []Account `json:"accounts"`
}

// Account 网页端账号配置
type Account struct {
	ID           string `json:"id"`
	ServiceToken string `json:"service_token"`
	UserID       string `json:"user_id"`
	Ph           string `json:"ph"`
	Active       bool   `json:"active"`
}

var (
	cfg  *Config
	mu   sync.RWMutex
	path string
)

func Load(p string) (*Config, error) {
	path = p
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg = &Config{Port: "8080", APIKey: "sk-mimo", DefaultModel: "mimo-v2.6-pro"}
			return cfg, Save()
		}
		return nil, err
	}
	cfg = &Config{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	// 环境变量覆盖（让 .env.example 名副其实）
	if v := os.Getenv("PORT"); v != "" {
		cfg.Port = v
	}
	if v := os.Getenv("API_KEY"); v != "" {
		cfg.APIKey = v
	}
	return cfg, nil
}

func Get() Config {
	mu.RLock()
	defer mu.RUnlock()
	return *cfg
}

func Save() error {
	mu.RLock()
	data, err := json.MarshalIndent(cfg, "", "  ")
	pathCopy := path
	mu.RUnlock()
	if err != nil {
		return err
	}
	return os.WriteFile(pathCopy, data, 0644)
}

func Update(fn func(*Config)) {
	mu.Lock()
	fn(cfg)
	mu.Unlock()
}
