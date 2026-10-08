package auth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"antigravity2api/pkg/config"
)

// TokenManager manages Google OAuth tokens, automated refresh, and project ID
type TokenManager struct {
	cfg        *config.Config
	httpClient *http.Client
	mu         sync.Mutex
}

func NewTokenManager(cfg *config.Config) *TokenManager {
	return &TokenManager{
		cfg:        cfg,
		httpClient: NewHTTPClient(cfg),
	}
}

// UpdateHTTPClient updates the internal client (e.g. if proxy changes)
func (m *TokenManager) UpdateHTTPClient() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.httpClient = NewHTTPClient(m.cfg)
}

// IsAuthenticated checks if a refresh token is present
func (m *TokenManager) IsAuthenticated() bool {
	return m.cfg.RefreshToken != ""
}

// GetAccessToken returns a valid access token, auto-refreshing if expired or near expiration
func (m *TokenManager) GetAccessToken(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cfg.RefreshToken == "" {
		return "", fmt.Errorf("no refresh token configured. Please authenticate first")
	}

	// Buffer 5 minutes before actual expiry
	now := time.Now().Unix()
	if m.cfg.AccessToken != "" && m.cfg.ExpiresAt > now+300 {
		return m.cfg.AccessToken, nil
	}

	fmt.Printf("[Auth] Refreshing access token via Google OAuth...\n")
	tokenResp, err := RefreshAccessToken(ctx, m.cfg.RefreshToken, m.httpClient)
	if err != nil {
		return "", fmt.Errorf("token refresh failed: %w", err)
	}

	newExpiry := time.Now().Unix() + tokenResp.ExpiresIn
	_ = m.cfg.Update(func(c *config.Config) {
		c.AccessToken = tokenResp.AccessToken
		c.ExpiresAt = newExpiry
		if tokenResp.RefreshToken != "" {
			c.RefreshToken = tokenResp.RefreshToken
		}
	})

	// Also ensure we have a ProjectID
	if m.cfg.ProjectID == "" {
		if pid, err := FetchProjectID(ctx, tokenResp.AccessToken, m.httpClient); err == nil {
			_ = m.cfg.Update(func(c *config.Config) {
				c.ProjectID = pid
			})
			fmt.Printf("[Auth] Acquired companion project ID: %s\n", pid)
		}
	}

	// Ensure we have UserEmail
	if m.cfg.UserEmail == "" {
		if email, err := FetchUserEmail(ctx, tokenResp.AccessToken, m.httpClient); err == nil {
			_ = m.cfg.Update(func(c *config.Config) {
				c.UserEmail = email
			})
		}
	}

	return m.cfg.AccessToken, nil
}

// GetProjectID returns the current project ID or fetches/generates one
func (m *TokenManager) GetProjectID(ctx context.Context) (string, error) {
	if m.cfg.ProjectID != "" {
		return m.cfg.ProjectID, nil
	}

	token, err := m.GetAccessToken(ctx)
	if err != nil {
		return GenerateRandomProjectID(), nil
	}

	pid, err := FetchProjectID(ctx, token, m.httpClient)
	if err != nil {
		pid = GenerateRandomProjectID()
	}
	_ = m.cfg.Update(func(c *config.Config) {
		c.ProjectID = pid
	})
	return pid, nil
}

// StartOAuthServer starts a temporary HTTP server on an available port to receive the OAuth redirect code
func (m *TokenManager) StartOAuthServer(ctx context.Context, onURLReady func(authURL string)) error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("failed to bind local port: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://localhost:%d/oauth-callback", port)
	authURL := BuildAuthURL(redirectURI)

	errChan := make(chan error, 1)
	mux := http.NewServeMux()

	server := &http.Server{Handler: mux}

	mux.HandleFunc("/oauth-callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			errMsg := r.URL.Query().Get("error")
			http.Error(w, "OAuth Error: "+errMsg, http.StatusBadRequest)
			errChan <- fmt.Errorf("oauth failed: %s", errMsg)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`
			<!DOCTYPE html>
			<html>
			<head><title>Authorization Successful</title></head>
			<body style="font-family: -apple-system, sans-serif; display: flex; justify-content: center; align-items: center; height: 90vh; background: #0f172a; color: #f8fafc;">
				<div style="text-align: center; background: #1e293b; padding: 40px; border-radius: 16px; box-shadow: 0 10px 25px rgba(0,0,0,0.5);">
					<h1 style="color: #10b981; margin-bottom: 8px;">🎉 授权成功！</h1>
					<p style="color: #94a3b8; font-size: 16px;">Antigravity 凭据已成功保存。你可以关闭此页面并返回应用程序了。</p>
				</div>
			</body>
			</html>
		`))

		go func() {
			time.Sleep(500 * time.Millisecond)
			_ = server.Shutdown(context.Background())
		}()

		// Exchange code
		tokenResp, err := ExchangeCode(ctx, code, redirectURI, m.httpClient)
		if err != nil {
			errChan <- fmt.Errorf("failed to exchange code: %w", err)
			return
		}

		newExpiry := time.Now().Unix() + tokenResp.ExpiresIn
		email, _ := FetchUserEmail(ctx, tokenResp.AccessToken, m.httpClient)
		pid, _ := FetchProjectID(ctx, tokenResp.AccessToken, m.httpClient)

		_ = m.cfg.Update(func(c *config.Config) {
			c.RefreshToken = tokenResp.RefreshToken
			c.AccessToken = tokenResp.AccessToken
			c.ExpiresAt = newExpiry
			c.UserEmail = email
			c.ProjectID = pid
		})

		fmt.Printf("[Auth] ✅ 登录成功！用户: %s, 项目ID: %s\n", email, pid)
		errChan <- nil
	})

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	if onURLReady != nil {
		onURLReady(authURL)
	}

	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		_ = server.Shutdown(context.Background())
		return ctx.Err()
	}
}
