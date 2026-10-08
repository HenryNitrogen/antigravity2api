package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// Config holds all server and authentication settings
type Config struct {
	Host           string `json:"host"`
	Port           int    `json:"port"`
	APIKey         string `json:"api_key,omitempty"`
	ProxyURL       string `json:"proxy_url,omitempty"`
	RefreshToken   string `json:"refresh_token,omitempty"`
	AccessToken    string `json:"access_token,omitempty"`
	ExpiresAt      int64  `json:"expires_at,omitempty"`
	ProjectID      string `json:"project_id,omitempty"`
	UserEmail      string `json:"user_email,omitempty"`
	DefaultModel   string `json:"default_model,omitempty"`
	ThinkingFormat string `json:"thinking_format,omitempty"` // "reasoning_content", "think_tag", or "both"

	configPath string       `json:"-"`
	mu         sync.RWMutex `json:"-"`
}

var (
	globalConfig *Config
	configOnce   sync.Once
)

// GetConfigPath returns the determined path for config.json
func GetConfigPath() string {
	if custom := os.Getenv("CONFIG_PATH"); custom != "" {
		return custom
	}
	if _, err := os.Stat("config.json"); err == nil {
		return "config.json"
	}
	home, err := os.UserHomeDir()
	if err == nil {
		dir := filepath.Join(home, ".config", "antigravity2api")
		_ = os.MkdirAll(dir, 0755)
		return filepath.Join(dir, "config.json")
	}
	return "config.json"
}

// LoadConfig loads or initializes configuration
func LoadConfig() (*Config, error) {
	path := GetConfigPath()
	cfg := &Config{
		Host:           "0.0.0.0",
		Port:           8088,
		ProjectID:      "aicode-consumers",
		DefaultModel:   "gemini-3.8-flash",
		ThinkingFormat: "reasoning_content",
		configPath:     path,
	}

	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, cfg); err != nil {
			fmt.Printf("[Config] Warning: failed to parse %s: %v\n", path, err)
		}
	}

	// Environment variable overrides
	if p := os.Getenv("PORT"); p != "" {
		if val, err := strconv.Atoi(p); err == nil {
			cfg.Port = val
		}
	} else if p := os.Getenv("AG2API_PORT"); p != "" {
		if val, err := strconv.Atoi(p); err == nil {
			cfg.Port = val
		}
	}

	if h := os.Getenv("HOST"); h != "" {
		cfg.Host = h
	}

	if proxy := os.Getenv("PROXY_URL"); proxy != "" {
		cfg.ProxyURL = proxy
	} else if proxy := os.Getenv("HTTP_PROXY"); proxy != "" && cfg.ProxyURL == "" {
		cfg.ProxyURL = proxy
	}

	if rt := os.Getenv("REFRESH_TOKEN"); rt != "" {
		cfg.RefreshToken = rt
	}

	if key := os.Getenv("API_KEY"); key != "" {
		cfg.APIKey = key
	}

	if pid := os.Getenv("PROJECT_ID"); pid != "" {
		cfg.ProjectID = pid
	} else if cfg.ProjectID == "" {
		cfg.ProjectID = "aicode-consumers"
	}

	if m := os.Getenv("DEFAULT_MODEL"); m != "" {
		cfg.DefaultModel = m
	}

	cfg.configPath = path
	globalConfig = cfg
	return cfg, nil
}

// Get returns the singleton config
func Get() *Config {
	configOnce.Do(func() {
		if globalConfig == nil {
			_, _ = LoadConfig()
		}
	})
	return globalConfig
}

// Save persists config to file
func (c *Config) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.configPath == "" {
		c.configPath = GetConfigPath()
	}

	dir := filepath.Dir(c.configPath)
	_ = os.MkdirAll(dir, 0755)

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	tmpFile := c.configPath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmpFile, c.configPath)
}

// Update updates fields safely and saves
func (c *Config) Update(fn func(c *Config)) error {
	c.mu.Lock()
	fn(c)
	c.mu.Unlock()
	return c.Save()
}
