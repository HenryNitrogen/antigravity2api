package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// AntigravityLiveModels contains the real Antigravity model IDs currently active in Antigravity.app
var AntigravityLiveModels = []string{
	"gemini-3.8-flash",
	"gemini-3.8-flash-high",
	"gemini-3.8-flash-medium",
	"gemini-3.8-flash-low",
	"gemini-3.7-flash",
	"gemini-3.7-flash-medium",
	"gemini-3.6-flash",
	"gemini-3.6-flash-medium",
	"gemini-3.5-flash",
	"gemini-3.1-pro",
	"gemini-3.1-pro-low",
	"gemini-3.1-pro-high",
	"gemini-3.1-flash-lite",
	"gemini-3.1-flash-image",
	"gemini-2.5-pro",
	"gemini-2.5-flash",
	"gpt-4o",
	"gpt-4o-mini",
	"claude-3-7-sonnet",
	"claude-3-5-sonnet",
}

var (
	dynamicModelsCache []string
	dynamicModelsMu    sync.RWMutex
)

// GetAvailableModels returns the dynamic model list if fetched, or the built-in live models
func GetAvailableModels() []string {
	dynamicModelsMu.RLock()
	defer dynamicModelsMu.RUnlock()
	if len(dynamicModelsCache) > 0 {
		return dynamicModelsCache
	}
	return AntigravityLiveModels
}

// SetDynamicModels updates the cached models from upstream
func SetDynamicModels(models []string) {
	dynamicModelsMu.Lock()
	defer dynamicModelsMu.Unlock()
	if len(models) > 0 {
		dynamicModelsCache = models
	}
}

// MapModel maps an incoming client model name to Google's internal model identifier
func MapModel(input string) string {
	lower := strings.ToLower(strings.TrimSpace(input))

	switch lower {
	// 3.8 系列 (当前最新主力模型)
	case "gemini-3.8-flash", "gemini-3.8", "gemini-3.8-flash-high", "3.8", "3.8-flash", "gemini-3.8-flash-tiered":
		return "gemini-3.8-flash-tiered"

	// 3.7 系列
	case "gemini-3.7-flash", "gemini-3.7", "gemini-3.7-flash-medium", "3.7", "3.7-flash", "gemini-3.7-flash-tiered":
		return "gemini-3.7-flash-tiered"

	// 3.6 系列
	case "gemini-3.6-flash", "gemini-3.6", "gemini-3.6-flash-medium", "3.6", "3.6-flash", "gemini-3.6-flash-tiered":
		return "gemini-3.6-flash-tiered"
	case "gemini-3.6-flash-high":
		return "gemini-3.6-flash-high"

	// 3.1 Pro 系列
	case "gemini-3.1-pro", "gemini-3.1", "gemini-3.1-pro-low", "3.1", "3.1-pro":
		return "gemini-3.1-pro-low"

	// Claude 系列
	case "claude-sonnet-4-6", "claude-3-7-sonnet", "claude-3-5-sonnet":
		return "claude-sonnet-4-6"

	// 兼容别名映射
	case "gpt-4o", "gpt-4", "gpt-4-turbo":
		return "gemini-3.8-flash-tiered"
	case "gpt-4o-mini", "gpt-3.5-turbo":
		return "gemini-3.6-flash-tiered"

	default:
		// 模糊匹配
		if strings.Contains(lower, "3.8") {
			return "gemini-3.8-flash-tiered"
		}
		if strings.Contains(lower, "3.7") {
			return "gemini-3.7-flash-tiered"
		}
		if strings.Contains(lower, "3.6") {
			return "gemini-3.6-flash-tiered"
		}
		if strings.Contains(lower, "3.1") {
			return "gemini-3.1-pro-low"
		}
		if strings.Contains(lower, "claude") {
			return "claude-sonnet-4-6"
		}
		return "gemini-3.8-flash-tiered"
	}
}

// FetchAvailableModelsFromUpstream calls Google's v1internal:fetchAvailableModels dynamically
func FetchAvailableModelsFromUpstream(ctx context.Context, accessToken string, httpClient *http.Client) ([]string, error) {
	url := "https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels"
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader("{}"))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", "antigravity/1.11.9 darwin/arm64")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetchAvailableModels returned %d", resp.StatusCode)
	}

	var data struct {
		Models map[string]any `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	var models []string
	for k := range data.Models {
		cleanID := strings.TrimPrefix(k, "models/")
		models = append(models, cleanID)
	}
	return models, nil
}
