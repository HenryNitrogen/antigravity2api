//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#include "gui.h"
#import <Cocoa/Cocoa.h>

static void copyTextToPasteboard(const char* text) {
    @autoreleasepool {
        NSString *str = [NSString stringWithUTF8String:text];
        NSPasteboard *pasteboard = [NSPasteboard generalPasteboard];
        [pasteboard clearContents];
        [pasteboard setString:str forType:NSPasteboardTypeString];
    }
}

static void openURLInBrowser(const char* urlStr) {
    @autoreleasepool {
        NSString *str = [NSString stringWithUTF8String:urlStr];
        [[NSWorkspace sharedWorkspace] openURL:[NSURL URLWithString:str]];
    }
}
*/
import "C"
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"antigravity2api/pkg/auth"
	"antigravity2api/pkg/config"
)

var (
	globalCfg          *config.Config
	globalTokenManager *auth.TokenManager
)

//export goOnCopyRefreshToken
func goOnCopyRefreshToken() {
	if globalCfg == nil || globalCfg.RefreshToken == "" {
		C.copyTextToPasteboard(C.CString("未找到 Refresh Token，请先登录！"))
		return
	}
	C.copyTextToPasteboard(C.CString(globalCfg.RefreshToken))
}

//export goOnCopyDockerCompose
func goOnCopyDockerCompose() {
	rt := "在这里填入你的_REFRESH_TOKEN"
	if globalCfg != nil && globalCfg.RefreshToken != "" {
		rt = globalCfg.RefreshToken
	}

	snippet := fmt.Sprintf(`services:
  antigravity2api:
    image: ghcr.io/henrynitrogen/antigravity2api:latest # 或本地 build: .
    container_name: antigravity2api
    restart: always
    ports:
      - "8088:8088"
    environment:
      - PORT=8088
      - HOST=0.0.0.0
      - REFRESH_TOKEN=%s
      - PROJECT_ID=aicode-consumers
      - DEFAULT_MODEL=gemini-3.8-flash
      # - PROXY_URL=http://host.docker.internal:7890 # 如果服务器在中国大陆，取消注释配置出海代理
`, rt)

	C.copyTextToPasteboard(C.CString(snippet))
}

//export goOnCopyBaseURL
func goOnCopyBaseURL() {
	port := 8088
	if globalCfg != nil && globalCfg.Port > 0 {
		port = globalCfg.Port
	}
	url := fmt.Sprintf("http://localhost:%d/v1", port)
	C.copyTextToPasteboard(C.CString(url))
}

//export goOnLoginRequested
func goOnLoginRequested() {
	if globalTokenManager == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	go func() {
		defer cancel()
		_ = globalTokenManager.StartOAuthServer(ctx, func(authURL string) {
			C.openURLInBrowser(C.CString(authURL))
		})
		refreshGUI()
	}()
}

//export goOnSendPrompt
func goOnSendPrompt(promptC *C.char, modelC *C.char) {
	prompt := C.GoString(promptC)
	model := C.GoString(modelC)
	if model == "" {
		model = "gemini-3.8-flash"
	}

	port := 8088
	if globalCfg != nil && globalCfg.Port > 0 {
		port = globalCfg.Port
	}

	reqBody := map[string]any{
		"model":  model,
		"stream": false,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	reqBytes, _ := json.Marshal(reqBody)

	url := fmt.Sprintf("http://127.0.0.1:%d/v1/chat/completions", port)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(reqBytes))
	if err != nil {
		msg := fmt.Sprintf("❌ 请求失败: %v", err)
		C.setGUIResponseText(C.CString(msg))
		return
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("❌ 服务端返回错误 (%d): %s", resp.StatusCode, string(bodyBytes))
		C.setGUIResponseText(C.CString(msg))
		return
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(bodyBytes, &parsed); err != nil {
		C.setGUIResponseText(C.CString(string(bodyBytes)))
		return
	}

	result := ""
	if len(parsed.Choices) > 0 {
		msg := parsed.Choices[0].Message
		if msg.ReasoningContent != "" {
			result += fmt.Sprintf("🧠 [思维链思考过程]:\n%s\n\n---------------------------\n", msg.ReasoningContent)
		}
		result += fmt.Sprintf("💬 [模型回答]:\n%s\n\n[Tokens: %d]", msg.Content, parsed.Usage.TotalTokens)
	} else {
		result = string(bodyBytes)
	}

	C.setGUIResponseText(C.CString(result))
}

func refreshGUI() {
	if globalCfg == nil || globalTokenManager == nil {
		return
	}
	cState := C.AppState{
		port:          C.int(globalCfg.Port),
		userEmail:     C.CString(globalCfg.UserEmail),
		projectID:     C.CString(globalCfg.ProjectID),
		refreshToken:  C.CString(globalCfg.RefreshToken),
		authenticated: 0,
	}
	if globalTokenManager.IsAuthenticated() {
		cState.authenticated = 1
	}
	C.updateGUIState(cState)
}

func launchNativeGUI(cfg *config.Config, tm *auth.TokenManager) {
	globalCfg = cfg
	globalTokenManager = tm

	cState := C.AppState{
		port:          C.int(cfg.Port),
		userEmail:     C.CString(cfg.UserEmail),
		projectID:     C.CString(cfg.ProjectID),
		refreshToken:  C.CString(cfg.RefreshToken),
		authenticated: 0,
	}
	if tm.IsAuthenticated() {
		cState.authenticated = 1
	}

	C.runMacGUI(cState)
}
