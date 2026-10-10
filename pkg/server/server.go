package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"antigravity2api/pkg/adapter"
	"antigravity2api/pkg/auth"
	"antigravity2api/pkg/config"
	"antigravity2api/pkg/upstream"
)

type LogEntry struct {
	Time     string `json:"time"`
	Method   string `json:"method"`
	Path     string `json:"path"`
	Model    string `json:"model,omitempty"`
	Status   int    `json:"status"`
	Duration int64  `json:"duration"`
	Tokens   int    `json:"tokens,omitempty"`
}

type Server struct {
	cfg            *config.Config
	tokenManager   *auth.TokenManager
	upstreamClient *upstream.UpstreamClient
	httpServer     *http.Server

	recentLogs []LogEntry
	logsMu     sync.RWMutex
}

func NewServer(cfg *config.Config, tm *auth.TokenManager, uc *upstream.UpstreamClient) *Server {
	return &Server{
		cfg:            cfg,
		tokenManager:   tm,
		upstreamClient: uc,
		recentLogs:     make([]LogEntry, 0, 50),
	}
}

func (s *Server) AddLog(entry LogEntry) {
	s.logsMu.Lock()
	defer s.logsMu.Unlock()
	if len(s.recentLogs) >= 50 {
		s.recentLogs = s.recentLogs[1:]
	}
	s.recentLogs = append(s.recentLogs, entry)
}

func (s *Server) GetLogs() []LogEntry {
	s.logsMu.RLock()
	defer s.logsMu.RUnlock()
	result := make([]LogEntry, len(s.recentLogs))
	copy(result, s.recentLogs)
	return result
}

func (s *Server) SetupHandler() http.Handler {
	mux := http.NewServeMux()

	// OpenAI Endpoints
	mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("/v1/models", s.handleModels)

	// Minimal Health Check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"service":"antigravity2api","status":"running","docs":"https://github.com/HenryNitrogen/antigravity2api"}`))
	})

	return s.corsMiddleware(mux)
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, x-api-key, anthropic-version")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) verifyAuth(r *http.Request) bool {
	if s.cfg.APIKey == "" {
		return true
	}
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		return token == s.cfg.APIKey
	}
	if apiKey := r.Header.Get("x-api-key"); apiKey != "" {
		return apiKey == s.cfg.APIKey
	}
	return false
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if r.Method != "POST" {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	if !s.verifyAuth(r) {
		http.Error(w, `{"error":{"message":"Unauthorized: Invalid API key"}}`, http.StatusUnauthorized)
		return
	}

	var req adapter.ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":{"message":"Invalid JSON: %v"}}`, err), http.StatusBadRequest)
		return
	}

	if req.Model == "" {
		req.Model = s.cfg.DefaultModel
	}

	inner, err := adapter.ConvertRequest(&req)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":{"message":"Request error: %v"}}`, err), http.StatusBadRequest)
		return
	}

	upstreamResp, err := s.upstreamClient.CallGenerateContent(r.Context(), inner, req.Model, req.Stream)
	if err != nil {
		fmt.Printf("[Error] Upstream error: %v\n", err)
		http.Error(w, fmt.Sprintf(`{"error":{"message":"Upstream error: %v"}}`, err), http.StatusBadGateway)
		s.AddLog(LogEntry{
			Time:     time.Now().Format("15:04:05"),
			Method:   r.Method,
			Path:     r.URL.Path,
			Model:    req.Model,
			Status:   http.StatusBadGateway,
			Duration: time.Since(start).Milliseconds(),
		})
		return
	}
	defer upstreamResp.Body.Close()

	if req.Stream {
		pTokens, cTokens, streamErr := adapter.ForwardStream(w, upstreamResp, req.Model, s.cfg)
		duration := time.Since(start).Milliseconds()
		fmt.Printf("[ChatStream] %s -> 200 OK (%dms, prompt: %d, comp: %d)\n", req.Model, duration, pTokens, cTokens)
		if streamErr != nil && streamErr != io.EOF {
			fmt.Printf("[ChatStream] Stream ended with notice: %v\n", streamErr)
		}
		s.AddLog(LogEntry{
			Time:     time.Now().Format("15:04:05"),
			Method:   r.Method,
			Path:     r.URL.Path,
			Model:    req.Model,
			Status:   http.StatusOK,
			Duration: duration,
			Tokens:   pTokens + cTokens,
		})
	} else {
		resp, err := adapter.HandleNonStream(upstreamResp, req.Model, s.cfg)
		duration := time.Since(start).Milliseconds()
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":{"message":"Parse error: %v"}}`, err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)

		var totalTokens int
		if resp.Usage != nil {
			totalTokens = resp.Usage.TotalTokens
		}
		fmt.Printf("[Chat] %s -> 200 OK (%dms, tokens: %d)\n", req.Model, duration, totalTokens)
		s.AddLog(LogEntry{
			Time:     time.Now().Format("15:04:05"),
			Method:   r.Method,
			Path:     r.URL.Path,
			Model:    req.Model,
			Status:   http.StatusOK,
			Duration: duration,
			Tokens:   totalTokens,
		})
	}
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	// Dynamically query upstream models if authenticated
	if s.tokenManager.IsAuthenticated() {
		if token, err := s.tokenManager.GetAccessToken(r.Context()); err == nil {
			if remoteModels, err := upstream.FetchAvailableModelsFromUpstream(r.Context(), token, auth.NewHTTPClient(s.cfg)); err == nil && len(remoteModels) > 0 {
				upstream.SetDynamicModels(remoteModels)
			}
		}
	}

	now := time.Now().Unix()
	var items []adapter.ModelItem
	for _, m := range upstream.GetAvailableModels() {
		items = append(items, adapter.ModelItem{
			ID:      m,
			Object:  "model",
			Created: now,
			OwnedBy: "antigravity",
		})
	}
	resp := adapter.ModelListResponse{
		Object: "list",
		Data:   items,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{
		"authenticated":   s.tokenManager.IsAuthenticated(),
		"port":            s.cfg.Port,
		"user_email":      s.cfg.UserEmail,
		"project_id":      s.cfg.ProjectID,
		"api_key":         s.cfg.APIKey,
		"proxy_url":       s.cfg.ProxyURL,
		"thinking_format": s.cfg.ThinkingFormat,
		"default_model":   s.cfg.DefaultModel,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Port           int    `json:"port"`
		APIKey         string `json:"api_key"`
		ProxyURL       string `json:"proxy_url"`
		ThinkingFormat string `json:"thinking_format"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	_ = s.cfg.Update(func(c *config.Config) {
		if body.Port > 0 {
			c.Port = body.Port
		}
		c.APIKey = body.APIKey
		c.ProxyURL = body.ProxyURL
		if body.ThinkingFormat != "" {
			c.ThinkingFormat = body.ThinkingFormat
		}
	})

	s.tokenManager.UpdateHTTPClient()
	s.upstreamClient.UpdateHTTPClient()

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	logs := s.GetLogs()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(logs)
}

func (s *Server) handleLoginURL(w http.ResponseWriter, r *http.Request) {
	// Redirect URI points back to this server's /oauth-callback
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	redirectURI := fmt.Sprintf("%s://%s/oauth-callback", scheme, r.Host)
	loginURL := auth.BuildAuthURL(redirectURI)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"url": loginURL,
	})
}

func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		errParam := r.URL.Query().Get("error")
		http.Error(w, "OAuth error: "+errParam, http.StatusBadRequest)
		return
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	redirectURI := fmt.Sprintf("%s://%s/oauth-callback", scheme, r.Host)

	httpClient := auth.NewHTTPClient(s.cfg)
	tokenResp, err := auth.ExchangeCode(r.Context(), code, redirectURI, httpClient)
	if err != nil {
		http.Error(w, "Failed to exchange code: "+err.Error(), http.StatusInternalServerError)
		return
	}

	email, _ := auth.FetchUserEmail(r.Context(), tokenResp.AccessToken, httpClient)
	pid, _ := auth.FetchProjectID(r.Context(), tokenResp.AccessToken, httpClient)

	_ = s.cfg.Update(func(c *config.Config) {
		c.RefreshToken = tokenResp.RefreshToken
		c.AccessToken = tokenResp.AccessToken
		c.ExpiresAt = time.Now().Unix() + tokenResp.ExpiresIn
		c.UserEmail = email
		c.ProjectID = pid
	})

	fmt.Printf("[Auth] ✅ OAuth 登录成功: %s (项目: %s)\n", email, pid)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`
		<!DOCTYPE html>
		<html>
		<head><title>Authorization Successful</title><meta http-equiv="refresh" content="2; url=/"></head>
		<body style="font-family: -apple-system, sans-serif; display: flex; justify-content: center; align-items: center; height: 90vh; background: #0b0f19; color: #f1f5f9;">
			<div style="text-align: center; background: #151d30; padding: 40px; border-radius: 16px; border: 1px solid #222f4c;">
				<h1 style="color: #10b981; margin-bottom: 8px;">🎉 授权成功！</h1>
				<p style="color: #94a3b8; font-size: 15px;">Google 账号已绑定并保存。2 秒后自动返回控制台...</p>
			</div>
		</body>
		</html>
	`))
}

func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: s.SetupHandler(),
	}

	fmt.Printf("\n🚀 Antigravity2Api 服务已启动: http://%s\n", addr)
	fmt.Printf("👉 OpenAI Chat 端点:   http://%s/v1/chat/completions\n", addr)
	fmt.Printf("👉 OpenAI Models 端点: http://%s/v1/models\n\n", addr)

	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}
