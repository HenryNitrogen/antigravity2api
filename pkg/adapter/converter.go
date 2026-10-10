package adapter

import (
	"encoding/json"
	"fmt"
	"strings"

	"antigravity2api/pkg/upstream"
)

// ExtractTextContent extracts text from string or structured OpenAI content parts
func ExtractTextContent(content any) string {
	if content == nil {
		return ""
	}
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var sb strings.Builder
		for _, part := range v {
			if m, ok := part.(map[string]any); ok {
				if m["type"] == "text" {
					if txt, ok := m["text"].(string); ok {
						sb.WriteString(txt)
					}
				}
			}
		}
		return sb.String()
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

// cleanJSONSchema cleans an incoming JSON Schema (OpenAI/JSON-Schema draft) to conform to Gemini protobuf Schema
func cleanJSONSchema(schema any) any {
	if schema == nil {
		return nil
	}
	switch v := schema.(type) {
	case []any:
		res := make([]any, len(v))
		for i, item := range v {
			res[i] = cleanJSONSchema(item)
		}
		return res

	case map[string]any:
		cleaned := make(map[string]any)

		// Keys not supported by Gemini v1internal Schema
		ignoredKeys := map[string]bool{
			"$schema":              true,
			"additionalProperties": true,
			"format":               true,
			"default":              true,
			"uniqueItems":          true,
			"propertyNames":        true,
			"title":                true,
		}

		for key, val := range v {
			if ignoredKeys[key] {
				continue
			}

			// Handle type field
			if key == "type" {
				switch tv := val.(type) {
				case string:
					cleaned["type"] = strings.ToUpper(tv)
				case []any:
					// e.g. ["string", "null"] -> pick non-null string
					var chosen string
					for _, item := range tv {
						if s, ok := item.(string); ok && s != "null" {
							chosen = s
							break
						}
					}
					if chosen == "" && len(tv) > 0 {
						if s, ok := tv[0].(string); ok {
							chosen = s
						}
					}
					if chosen == "" {
						chosen = "STRING"
					}
					cleaned["type"] = strings.ToUpper(chosen)
				default:
					cleaned["type"] = "STRING"
				}
				continue
			}

			// Handle const -> enum
			if key == "const" {
				cleaned["enum"] = []any{val}
				continue
			}

			// Recursively clean sub-schemas (properties, items, etc.)
			cleaned[key] = cleanJSONSchema(val)
		}

		return cleaned

	default:
		return v
	}
}

// ConvertRequest transforms an OpenAI ChatCompletionRequest to Gemini InternalInner
func ConvertRequest(req *ChatCompletionRequest) (upstream.InternalInner, error) {
	var systemTexts []string
	var contents []upstream.GeminiContent

	// Track tool_call id -> function name for matching role: "tool" responses
	toolIDToName := make(map[string]string)

	for _, msg := range req.Messages {
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		text := ExtractTextContent(msg.Content)

		switch role {
		case "system":
			if text != "" {
				systemTexts = append(systemTexts, text)
			}

		case "user":
			contents = append(contents, upstream.GeminiContent{
				Role:  "user",
				Parts: []upstream.GeminiPart{{Text: text}},
			})

		case "assistant":
			var parts []upstream.GeminiPart
			if msg.ReasoningContent != "" {
				parts = append(parts, upstream.GeminiPart{
					Text:    msg.ReasoningContent,
					Thought: true,
				})
			}
			if text != "" {
				parts = append(parts, upstream.GeminiPart{
					Text: text,
				})
			}

			// Handle OpenAI assistant message containing tool_calls
			for _, tc := range msg.ToolCalls {
				var argsMap map[string]any
				if tc.Function.Arguments != "" {
					_ = json.Unmarshal([]byte(tc.Function.Arguments), &argsMap)
				}
				if argsMap == nil {
					argsMap = make(map[string]any)
				}

				if tc.ID != "" && tc.Function.Name != "" {
					toolIDToName[tc.ID] = tc.Function.Name
				}

				parts = append(parts, upstream.GeminiPart{
					FunctionCall: &upstream.GeminiFunctionCall{
						Name: tc.Function.Name,
						Args: argsMap,
						ID:   tc.ID,
					},
				})
			}

			if len(parts) == 0 {
				parts = []upstream.GeminiPart{{Text: ""}}
			}
			contents = append(contents, upstream.GeminiContent{
				Role:  "model",
				Parts: parts,
			})

		case "tool":
			// Tool output sent back by client
			funcName := msg.Name
			if funcName == "" && msg.ToolCallID != "" {
				funcName = toolIDToName[msg.ToolCallID]
			}
			if funcName == "" {
				funcName = msg.ToolCallID
			}
			if funcName == "" {
				funcName = "tool_response"
			}

			var respPayload map[string]any
			// Try to unmarshal JSON object if returned by tool, else wrap as string result
			if err := json.Unmarshal([]byte(text), &respPayload); err != nil || respPayload == nil {
				respPayload = map[string]any{"result": text}
			}

			// In Gemini, role: "user" carries the functionResponse
			contents = append(contents, upstream.GeminiContent{
				Role: "user",
				Parts: []upstream.GeminiPart{
					{
						FunctionResponse: &upstream.GeminiFunctionReturn{
							Name:     funcName,
							Response: respPayload,
							ID:       msg.ToolCallID,
						},
					},
				},
			})
		}
	}

	if len(contents) == 0 {
		return upstream.InternalInner{}, fmt.Errorf("messages cannot be empty")
	}

	inner := upstream.InternalInner{
		Contents: contents,
	}

	// 1. System instructions
	if len(systemTexts) > 0 {
		combinedSystem := strings.Join(systemTexts, "\n\n")
		inner.SystemInstruction = &upstream.GeminiInstruction{
			Parts: []upstream.GeminiPart{{Text: combinedSystem}},
		}
	}

	// 2. Tools & ToolChoice mapping
	if len(req.Tools) > 0 {
		var funcDecls []upstream.FunctionDeclaration
		for _, tool := range req.Tools {
			if tool.Function.Name == "" {
				continue
			}

			var cleanedParams map[string]any
			if tool.Function.Parameters != nil {
				if cleaned, ok := cleanJSONSchema(tool.Function.Parameters).(map[string]any); ok {
					cleanedParams = cleaned
				} else {
					cleanedParams = tool.Function.Parameters
				}
			}

			funcDecls = append(funcDecls, upstream.FunctionDeclaration{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Parameters:  cleanedParams,
			})
		}

		if len(funcDecls) > 0 {
			inner.Tools = []upstream.GeminiTool{
				{
					FunctionDeclarations: funcDecls,
				},
			}
			inner.ToolConfig = &upstream.GeminiToolConfig{
				FunctionCallingConfig: &upstream.FunctionCallingConfig{
					Mode: "AUTO",
				},
			}

			// Handle tool_choice if specified (e.g., "none", "auto", "required", or {"type": "function", "function": {"name": "..."}})
			if req.ToolChoice != nil {
				switch tc := req.ToolChoice.(type) {
				case string:
					switch strings.ToLower(tc) {
					case "none":
						inner.ToolConfig.FunctionCallingConfig.Mode = "NONE"
					case "required":
						inner.ToolConfig.FunctionCallingConfig.Mode = "ANY"
					case "auto":
						inner.ToolConfig.FunctionCallingConfig.Mode = "AUTO"
					}
				case map[string]any:
					if f, ok := tc["function"].(map[string]any); ok {
						if name, ok := f["name"].(string); ok && name != "" {
							inner.ToolConfig.FunctionCallingConfig.Mode = "ANY"
							inner.ToolConfig.FunctionCallingConfig.AllowedFunctionNames = []string{name}
						}
					}
				}
			}
		}
	}

	// 3. Generation Config
	genCfg := &upstream.GenerationConfig{
		MaxOutputTokens: 65535,
		ThinkingConfig: &upstream.ThinkingConfig{
			ThinkingBudget: -1,
		},
	}
	if req.Temperature != nil {
		genCfg.Temperature = req.Temperature
	}
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		genCfg.MaxOutputTokens = *req.MaxTokens
	}
	inner.GenerationConfig = genCfg

	return inner, nil
}
