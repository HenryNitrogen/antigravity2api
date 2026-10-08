package adapter

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"antigravity2api/pkg/config"
)

type UpstreamEnvelope struct {
	Response      *UpstreamResponse `json:"response"`
	Candidates    []Candidate       `json:"candidates"`
	UsageMetadata *UsageMetadata    `json:"usageMetadata"`
	ResponseID    string            `json:"responseId"`
}

type UpstreamResponse struct {
	Candidates    []Candidate    `json:"candidates"`
	UsageMetadata *UsageMetadata `json:"usageMetadata"`
	ResponseID    string         `json:"responseId"`
}

type Candidate struct {
	Content      *CandidateContent `json:"content"`
	FinishReason *string           `json:"finishReason"`
}

type CandidateContent struct {
	Role  string                 `json:"role"`
	Parts []UpstreamResponsePart `json:"parts"`
}

type UpstreamResponsePart struct {
	Text             string `json:"text"`
	Thought          bool   `json:"thought"`
	ThoughtSignature string `json:"thoughtSignature"`
}

type UsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

// ForwardStream processes upstream SSE lines and writes OpenAI-compatible SSE chunks to client
func ForwardStream(w http.ResponseWriter, upstreamResp *http.Response, model string, cfg *config.Config) (int, int, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return 0, 0, fmt.Errorf("response writer does not support streaming")
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	chunkID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	created := time.Now().Unix()

	var streamBody io.Reader = upstreamResp.Body
	if strings.EqualFold(upstreamResp.Header.Get("Content-Encoding"), "gzip") {
		if gz, err := gzip.NewReader(upstreamResp.Body); err == nil {
			defer gz.Close()
			streamBody = gz
		}
	}

	scanner := bufio.NewScanner(streamBody)
	// Support up to 4MB SSE buffer
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)

	inThinking := false
	hadThinking := false
	totalPromptTokens := 0
	totalCompletionTokens := 0

	thinkingFormat := cfg.ThinkingFormat
	if thinkingFormat == "" {
		thinkingFormat = "reasoning_content"
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}

		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}

		var env UpstreamEnvelope
		if err := json.Unmarshal([]byte(payload), &env); err != nil {
			continue
		}

		candidates := env.Candidates
		if env.Response != nil && len(env.Response.Candidates) > 0 {
			candidates = env.Response.Candidates
		}

		if env.UsageMetadata != nil {
			totalPromptTokens = env.UsageMetadata.PromptTokenCount
			totalCompletionTokens = env.UsageMetadata.CandidatesTokenCount
		} else if env.Response != nil && env.Response.UsageMetadata != nil {
			totalPromptTokens = env.Response.UsageMetadata.PromptTokenCount
			totalCompletionTokens = env.Response.UsageMetadata.CandidatesTokenCount
		}

		for _, cand := range candidates {
			if cand.Content == nil {
				continue
			}

			for _, part := range cand.Content.Parts {
				txt := part.Text

				if part.Thought {
					hadThinking = true
					// First thought chunk: emit <think> tag if configured
					if !inThinking {
						inThinking = true
						if thinkingFormat == "think_tag" || thinkingFormat == "both" {
							sendChunk(w, chunkID, created, model, ChunkDelta{Content: "<think>\n"})
							flusher.Flush()
						}
					}

					delta := ChunkDelta{}
					if thinkingFormat == "reasoning_content" || thinkingFormat == "both" {
						delta.ReasoningContent = txt
					}
					if thinkingFormat == "think_tag" {
						delta.Content = txt
					}

					sendChunk(w, chunkID, created, model, delta)
					flusher.Flush()
				} else {
					// Normal text chunk
					if inThinking {
						inThinking = false
						if thinkingFormat == "think_tag" || thinkingFormat == "both" {
							sendChunk(w, chunkID, created, model, ChunkDelta{Content: "\n</think>\n\n"})
							flusher.Flush()
						}
					}

					if txt != "" {
						sendChunk(w, chunkID, created, model, ChunkDelta{Content: txt})
						flusher.Flush()
					}
				}
			}
		}
	}

	// If stream finished while still in thinking block
	if inThinking && (thinkingFormat == "think_tag" || thinkingFormat == "both") {
		sendChunk(w, chunkID, created, model, ChunkDelta{Content: "\n</think>\n\n"})
		flusher.Flush()
	}

	// Final stop chunk
	stopReason := "stop"
	finalChunk := ChatCompletionChunk{
		ID:      chunkID,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model,
		Choices: []ChunkChoice{
			{
				Index:        0,
				Delta:        ChunkDelta{},
				FinishReason: &stopReason,
			},
		},
	}
	chunkBytes, _ := json.Marshal(finalChunk)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", chunkBytes)
	_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()

	_ = hadThinking
	return totalPromptTokens, totalCompletionTokens, scanner.Err()
}

func sendChunk(w io.Writer, id string, created int64, model string, delta ChunkDelta) {
	chunk := ChatCompletionChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model,
		Choices: []ChunkChoice{
			{
				Index: 0,
				Delta: delta,
			},
		},
	}
	bytesData, _ := json.Marshal(chunk)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", bytesData)
}

// HandleNonStream aggregates SSE or JSON into a single ChatCompletionResponse
func HandleNonStream(upstreamResp *http.Response, model string, cfg *config.Config) (*ChatCompletionResponse, error) {
	var fullText strings.Builder
	var fullReasoning strings.Builder
	totalPromptTokens := 0
	totalCompletionTokens := 0

	var reader io.Reader = upstreamResp.Body
	if strings.EqualFold(upstreamResp.Header.Get("Content-Encoding"), "gzip") {
		if gz, err := gzip.NewReader(upstreamResp.Body); err == nil {
			defer gz.Close()
			reader = gz
		}
	}

	bodyBytes, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	// First attempt to parse the entire body as a single JSON object (standard non-stream response)
	var singleEnv UpstreamEnvelope
	if err := json.Unmarshal(bodyBytes, &singleEnv); err == nil && (singleEnv.Response != nil || len(singleEnv.Candidates) > 0) {
		candidates := singleEnv.Candidates
		if singleEnv.Response != nil && len(singleEnv.Response.Candidates) > 0 {
			candidates = singleEnv.Response.Candidates
		}

		if singleEnv.UsageMetadata != nil {
			totalPromptTokens = singleEnv.UsageMetadata.PromptTokenCount
			totalCompletionTokens = singleEnv.UsageMetadata.CandidatesTokenCount
		} else if singleEnv.Response != nil && singleEnv.Response.UsageMetadata != nil {
			totalPromptTokens = singleEnv.Response.UsageMetadata.PromptTokenCount
			totalCompletionTokens = singleEnv.Response.UsageMetadata.CandidatesTokenCount
		}

		for _, cand := range candidates {
			if cand.Content == nil {
				continue
			}
			for _, part := range cand.Content.Parts {
				if part.Thought {
					fullReasoning.WriteString(part.Text)
				} else {
					fullText.WriteString(part.Text)
				}
			}
		}
	} else {
		// Fallback: parse as SSE lines
		lines := strings.Split(string(bodyBytes), "\n")
		for _, rawLine := range lines {
			line := strings.TrimSpace(rawLine)
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, "data:") {
				line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
			if line == "[DONE]" {
				break
			}

			var env UpstreamEnvelope
			if err := json.Unmarshal([]byte(line), &env); err != nil {
				continue
			}

			candidates := env.Candidates
			if env.Response != nil && len(env.Response.Candidates) > 0 {
				candidates = env.Response.Candidates
			}

			if env.UsageMetadata != nil {
				totalPromptTokens = env.UsageMetadata.PromptTokenCount
				totalCompletionTokens = env.UsageMetadata.CandidatesTokenCount
			} else if env.Response != nil && env.Response.UsageMetadata != nil {
				totalPromptTokens = env.Response.UsageMetadata.PromptTokenCount
				totalCompletionTokens = env.Response.UsageMetadata.CandidatesTokenCount
			}

			for _, cand := range candidates {
				if cand.Content == nil {
					continue
				}
				for _, part := range cand.Content.Parts {
					if part.Thought {
						fullReasoning.WriteString(part.Text)
					} else {
						fullText.WriteString(part.Text)
					}
				}
			}
		}
	}

	content := fullText.String()
	reasoning := fullReasoning.String()

	if cfg.ThinkingFormat == "think_tag" && reasoning != "" {
		content = fmt.Sprintf("<think>\n%s\n</think>\n\n%s", reasoning, content)
	}

	stopReason := "stop"
	resp := &ChatCompletionResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []ChatChoice{
			{
				Index: 0,
				Message: RespMessage{
					Role:             "assistant",
					Content:          content,
					ReasoningContent: reasoning,
				},
				FinishReason: &stopReason,
			},
		},
		Usage: &UsageInfo{
			PromptTokens:     totalPromptTokens,
			CompletionTokens: totalCompletionTokens,
			TotalTokens:      totalPromptTokens + totalCompletionTokens,
		},
	}

	return resp, nil
}
