package adapter

import (
	"net/http/httptest"
	"strings"
	"testing"

	"antigravity2api/pkg/config"
)

func TestConvertRequest_WithToolsAndResponses(t *testing.T) {
	req := &ChatCompletionRequest{
		Model: "gemini-3.8-flash",
		Messages: []ChatMessage{
			{
				Role:    "system",
				Content: "You are a helpful assistant.",
			},
			{
				Role:    "user",
				Content: "What is the weather in Tokyo?",
			},
			{
				Role: "assistant",
				ToolCalls: []ToolCall{
					{
						ID:   "call_12345",
						Type: "function",
						Function: FunctionCallData{
							Name:      "get_weather",
							Arguments: `{"location":"Tokyo"}`,
						},
					},
				},
			},
			{
				Role:       "tool",
				ToolCallID: "call_12345",
				Content:    `{"temperature":"22C","condition":"Sunny"}`,
			},
		},
		Tools: []Tool{
			{
				Type: "function",
				Function: FunctionPayload{
					Name:        "get_weather",
					Description: "Get the current weather",
					Parameters: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"location": map[string]any{
								"type": "string",
							},
						},
					},
				},
			},
		},
	}

	inner, err := ConvertRequest(req)
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	// 1. Verify system instruction
	if inner.SystemInstruction == nil || len(inner.SystemInstruction.Parts) == 0 {
		t.Errorf("expected SystemInstruction, got nil")
	} else if inner.SystemInstruction.Parts[0].Text != "You are a helpful assistant." {
		t.Errorf("unexpected SystemInstruction text: %s", inner.SystemInstruction.Parts[0].Text)
	}

	// 2. Verify tools mapping
	if len(inner.Tools) != 1 || len(inner.Tools[0].FunctionDeclarations) != 1 {
		t.Fatalf("expected 1 function declaration, got %v", inner.Tools)
	}
	fn := inner.Tools[0].FunctionDeclarations[0]
	if fn.Name != "get_weather" {
		t.Errorf("expected function name get_weather, got %s", fn.Name)
	}
	if inner.ToolConfig == nil || inner.ToolConfig.FunctionCallingConfig.Mode != "AUTO" {
		t.Errorf("expected ToolConfig AUTO, got %v", inner.ToolConfig)
	}

	// 3. Verify messages conversion
	if len(inner.Contents) != 3 {
		t.Fatalf("expected 3 contents (user, model, user/tool_resp), got %d", len(inner.Contents))
	}

	// Content 0: user text
	if inner.Contents[0].Role != "user" || inner.Contents[0].Parts[0].Text != "What is the weather in Tokyo?" {
		t.Errorf("unexpected content 0: %+v", inner.Contents[0])
	}

	// Content 1: assistant function call
	if inner.Contents[1].Role != "model" {
		t.Errorf("unexpected content 1 role: %s", inner.Contents[1].Role)
	}
	fcPart := inner.Contents[1].Parts[0].FunctionCall
	if fcPart == nil || fcPart.Name != "get_weather" || fcPart.ID != "call_12345" {
		t.Errorf("unexpected function call part: %+v", fcPart)
	}
	if loc, ok := fcPart.Args["location"].(string); !ok || loc != "Tokyo" {
		t.Errorf("unexpected function call args: %+v", fcPart.Args)
	}

	// Content 2: tool response
	if inner.Contents[2].Role != "user" {
		t.Errorf("unexpected content 2 role: %s", inner.Contents[2].Role)
	}
	respPart := inner.Contents[2].Parts[0].FunctionResponse
	if respPart == nil || respPart.Name != "get_weather" || respPart.ID != "call_12345" {
		t.Errorf("unexpected function response part: %+v", respPart)
	}
	if cond, ok := respPart.Response["condition"].(string); !ok || cond != "Sunny" {
		t.Errorf("unexpected function response data: %+v", respPart.Response)
	}
}

func TestHandleNonStream_ToolCallResponse(t *testing.T) {
	mockJSON := `{
		"candidates": [
			{
				"content": {
					"role": "model",
					"parts": [
						{
							"thought": true,
							"text": "The user is asking about the weather. Let me call get_weather."
						},
						{
							"functionCall": {
								"id": "call_tokyo_999",
								"name": "get_weather",
								"args": {
									"location": "Tokyo"
								}
							}
						}
					]
				},
				"finishReason": "STOP"
			}
		],
		"usageMetadata": {
			"promptTokenCount": 15,
			"candidatesTokenCount": 20,
			"totalTokenCount": 35
		}
	}`

	respRec := httptest.NewRecorder()
	respRec.Header().Set("Content-Type", "application/json")
	respRec.WriteHeader(200)
	_, _ = respRec.WriteString(mockJSON)
	httpResp := respRec.Result()

	cfg := &config.Config{
		ThinkingFormat: "reasoning_content",
	}

	chatResp, err := HandleNonStream(httpResp, "gemini-3.8-flash", cfg)
	if err != nil {
		t.Fatalf("HandleNonStream failed: %v", err)
	}

	if len(chatResp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(chatResp.Choices))
	}

	choice := chatResp.Choices[0]
	if choice.FinishReason == nil || *choice.FinishReason != "tool_calls" {
		t.Errorf("expected finish_reason 'tool_calls', got %v", choice.FinishReason)
	}

	if len(choice.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(choice.Message.ToolCalls))
	}

	tc := choice.Message.ToolCalls[0]
	if tc.ID != "call_tokyo_999" || tc.Function.Name != "get_weather" {
		t.Errorf("unexpected tool call: %+v", tc)
	}
	if !strings.Contains(tc.Function.Arguments, `"Tokyo"`) {
		t.Errorf("unexpected arguments: %s", tc.Function.Arguments)
	}
	if choice.Message.ReasoningContent != "The user is asking about the weather. Let me call get_weather." {
		t.Errorf("unexpected reasoning: %s", choice.Message.ReasoningContent)
	}
}

func TestForwardStream_ToolCallResponse(t *testing.T) {
	ssePayload := `data: {"candidates":[{"content":{"role":"model","parts":[{"thought":true,"text":"Thinking..."}]}}]}

data: {"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"id":"call_abc","name":"calculator","args":{"expr":"2+2"}}}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5}}

data: [DONE]
`

	respRec := httptest.NewRecorder()
	respRec.Header().Set("Content-Type", "text/event-stream")
	respRec.WriteHeader(200)
	_, _ = respRec.WriteString(ssePayload)
	httpResp := respRec.Result()

	wRec := httptest.NewRecorder()
	cfg := &config.Config{
		ThinkingFormat: "reasoning_content",
	}

	_, _, err := ForwardStream(wRec, httpResp, "gemini-3.8-flash", cfg)
	if err != nil {
		t.Fatalf("ForwardStream failed: %v", err)
	}

	output := wRec.Body.String()
	if !strings.Contains(output, "calculator") {
		t.Errorf("expected output to contain 'calculator', got: %s", output)
	}
	if !strings.Contains(output, "tool_calls") {
		t.Errorf("expected output to contain finish_reason 'tool_calls', got: %s", output)
	}
	if !strings.Contains(output, "Thinking...") {
		t.Errorf("expected output to contain reasoning chunk, got: %s", output)
	}
}
