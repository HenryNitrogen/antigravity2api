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

// ConvertRequest transforms an OpenAI ChatCompletionRequest to Gemini InternalInner
func ConvertRequest(req *ChatCompletionRequest) (upstream.InternalInner, error) {
	var systemTexts []string
	var contents []upstream.GeminiContent

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
			if len(parts) == 0 {
				parts = []upstream.GeminiPart{{Text: ""}}
			}
			contents = append(contents, upstream.GeminiContent{
				Role:  "model",
				Parts: parts,
			})
		}
	}

	if len(contents) == 0 {
		return upstream.InternalInner{}, fmt.Errorf("messages cannot be empty")
	}

	inner := upstream.InternalInner{
		Contents: contents,
	}

	if len(systemTexts) > 0 {
		combinedSystem := strings.Join(systemTexts, "\n\n")
		inner.SystemInstruction = &upstream.GeminiInstruction{
			Parts: []upstream.GeminiPart{{Text: combinedSystem}},
		}
	}

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
