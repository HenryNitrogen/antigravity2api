package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"antigravity2api/pkg/adapter"
	"antigravity2api/pkg/auth"
	"antigravity2api/pkg/config"
	"antigravity2api/pkg/upstream"
)

func TestLiveTokenAndUpstream(t *testing.T) {
	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	tokenManager := auth.NewTokenManager(cfg)
	if !tokenManager.IsAuthenticated() {
		t.Fatalf("未检测到有效认证信息")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	accessToken, err := tokenManager.GetAccessToken(ctx)
	if err != nil {
		t.Fatalf("获取 Access Token 失败: %v", err)
	}

	fmt.Printf("\n[Test] ✅ 成功获取 Access Token (前25位): %s...\n", accessToken[:25])

	client := upstream.NewUpstreamClient(cfg, tokenManager)

	// 1. 测试常规生成
	inner := upstream.InternalInner{
		Contents: []upstream.GeminiContent{
			{
				Role:  "user",
				Parts: []upstream.GeminiPart{{Text: "请只回答两个字：收到"}},
			},
		},
	}

	resp, err := client.CallGenerateContent(ctx, inner, "gemini-3.8-flash", false)
	if err != nil {
		t.Fatalf("调用上游 API 失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("上游响应错误 (%d): %s", resp.StatusCode, string(body))
	}

	parsed, err := adapter.HandleNonStream(resp, "gemini-3.8-flash", cfg)
	if err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	if len(parsed.Choices) == 0 {
		t.Fatalf("无 choices 返回")
	}

	fmt.Printf("[Test] ✅ 模型正常回复: %s\n", strings.TrimSpace(parsed.Choices[0].Message.Content))

	// 2. 测试真实 Tool Calling 上游调用
	innerWithTool := upstream.InternalInner{
		Contents: []upstream.GeminiContent{
			{
				Role:  "user",
				Parts: []upstream.GeminiPart{{Text: "帮我查询纽约现在的天气"}},
			},
		},
		Tools: []upstream.GeminiTool{
			{
				FunctionDeclarations: []upstream.FunctionDeclaration{
					{
						Name:        "get_weather",
						Description: "获取指定城市天气",
						Parameters: map[string]any{
							"type": "object",
							"properties": map[string]any{
								"city": map[string]any{"type": "string"},
							},
							"required": []string{"city"},
						},
					},
				},
			},
		},
		ToolConfig: &upstream.GeminiToolConfig{
			FunctionCallingConfig: &upstream.FunctionCallingConfig{
				Mode: "ANY",
			},
		},
	}

	respTool, err := client.CallGenerateContent(ctx, innerWithTool, "gemini-3.8-flash", false)
	if err != nil {
		t.Fatalf("调用 Tool 上游失败: %v", err)
	}
	defer respTool.Body.Close()

	if respTool.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(respTool.Body)
		t.Fatalf("上游 Tool 响应错误 (%d): %s", respTool.StatusCode, string(body))
	}

	parsedTool, err := adapter.HandleNonStream(respTool, "gemini-3.8-flash", cfg)
	if err != nil {
		t.Fatalf("解析 Tool 响应失败: %v", err)
	}

	if len(parsedTool.Choices) == 0 || len(parsedTool.Choices[0].Message.ToolCalls) == 0 {
		t.Fatalf("预期返回 Tool Calls，实际未返回: %+v", parsedTool.Choices)
	}

	tc := parsedTool.Choices[0].Message.ToolCalls[0]
	fmt.Printf("[Test] ✅ Tool Calling 正常触发: 函数名=%s, 参数=%s, ID=%s\n", tc.Function.Name, tc.Function.Arguments, tc.ID)

	// 3. 测试多轮交互：客户端带上 Tool Result 回传给模型
	chatReqRound2 := &adapter.ChatCompletionRequest{
		Model: "gemini-3.8-flash",
		Messages: []adapter.ChatMessage{
			{Role: "user", Content: "帮我查询纽约现在的天气"},
			{
				Role:      "assistant",
				ToolCalls: []adapter.ToolCall{tc},
			},
			{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    `{"temperature": "20°C", "condition": "晴朗"}`,
			},
		},
		Tools: []adapter.Tool{
			{
				Type: "function",
				Function: adapter.FunctionPayload{
					Name:        "get_weather",
					Description: "获取指定城市天气",
					Parameters: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"city": map[string]any{"type": "string"},
						},
						"required": []string{"city"},
					},
				},
			},
		},
	}

	innerRound2, err := adapter.ConvertRequest(chatReqRound2)
	if err != nil {
		t.Fatalf("转换 Round2 请求失败: %v", err)
	}

	respRound2, err := client.CallGenerateContent(ctx, innerRound2, "gemini-3.8-flash", false)
	if err != nil {
		t.Fatalf("多轮 Tool 回传失败 (Upstream Error): %v", err)
	}
	defer respRound2.Body.Close()

	if respRound2.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(respRound2.Body)
		t.Fatalf("上游 Round2 响应错误 (%d): %s", respRound2.StatusCode, string(body))
	}

	parsedRound2, err := adapter.HandleNonStream(respRound2, "gemini-3.8-flash", cfg)
	if err != nil {
		t.Fatalf("解析 Round2 响应失败: %v", err)
	}

	if len(parsedRound2.Choices) == 0 {
		t.Fatalf("Round2 无 choices 返回")
	}

	fmt.Printf("[Test] ✅ 多轮 Tool 交互完美成功！模型最终回复: %s\n", strings.TrimSpace(parsedRound2.Choices[0].Message.Content))
}
