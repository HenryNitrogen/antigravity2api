package upstream

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"antigravity2api/pkg/auth"
	"antigravity2api/pkg/config"
)

type UpstreamClient struct {
	cfg          *config.Config
	tokenManager *auth.TokenManager
	httpClient   *http.Client
}

func NewUpstreamClient(cfg *config.Config, tm *auth.TokenManager) *UpstreamClient {
	return &UpstreamClient{
		cfg:          cfg,
		tokenManager: tm,
		httpClient:   auth.NewHTTPClient(cfg),
	}
}

func (c *UpstreamClient) UpdateHTTPClient() {
	c.httpClient = auth.NewHTTPClient(c.cfg)
}

// GenerateContentRequest represents the payload for v1internal:generateContent
type GenerateContentRequest struct {
	Project     string         `json:"project"`
	RequestID   string         `json:"requestId"`
	Request     InternalInner  `json:"request"`
	Model       string         `json:"model"`
	UserAgent   string         `json:"userAgent"`
	RequestType string         `json:"requestType"`
}

type InternalInner struct {
	Contents          []GeminiContent    `json:"contents"`
	SystemInstruction *GeminiInstruction `json:"systemInstruction,omitempty"`
	GenerationConfig  *GenerationConfig  `json:"generationConfig,omitempty"`
}

type GeminiContent struct {
	Role  string       `json:"role"` // "user" or "model"
	Parts []GeminiPart `json:"parts"`
}

type GeminiInstruction struct {
	Parts []GeminiPart `json:"parts"`
}

type GeminiPart struct {
	Text             string `json:"text,omitempty"`
	Thought          bool   `json:"thought,omitempty"`
	ThoughtSignature string `json:"thoughtSignature,omitempty"`
}

type GenerationConfig struct {
	MaxOutputTokens int             `json:"maxOutputTokens,omitempty"`
	Temperature     *float64        `json:"temperature,omitempty"`
	ThinkingConfig  *ThinkingConfig `json:"thinkingConfig,omitempty"`
}

type ThinkingConfig struct {
	ThinkingBudget int `json:"thinkingBudget,omitempty"` // -1 for max/high
}

func generateUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// CallGenerateContent executes the upstream request to Google v1internal
func (c *UpstreamClient) CallGenerateContent(ctx context.Context, inner InternalInner, clientModel string, stream bool) (*http.Response, error) {
	token, err := c.tokenManager.GetAccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("authentication error: %w", err)
	}

	projectID, err := c.tokenManager.GetProjectID(ctx)
	if err != nil {
		projectID = auth.GenerateRandomProjectID()
	}

	mappedModel := MapModel(clientModel)

	// Default generation config
	if inner.GenerationConfig == nil {
		inner.GenerationConfig = &GenerationConfig{
			MaxOutputTokens: 65535,
			ThinkingConfig: &ThinkingConfig{
				ThinkingBudget: -1,
			},
		}
	} else {
		if inner.GenerationConfig.MaxOutputTokens <= 0 {
			inner.GenerationConfig.MaxOutputTokens = 65535
		}
		if inner.GenerationConfig.ThinkingConfig == nil {
			inner.GenerationConfig.ThinkingConfig = &ThinkingConfig{
				ThinkingBudget: -1,
			}
		}
	}

	reqPayload := GenerateContentRequest{
		Project:     projectID,
		RequestID:   fmt.Sprintf("agent-%s", generateUUID()),
		Request:     inner,
		Model:       mappedModel,
		UserAgent:   "antigravity",
		RequestType: "agent",
	}

	bodyBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode request: %w", err)
	}

	url := auth.V1InternalURL + ":generateContent"
	if stream {
		url += "?alt=sse"
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "antigravity/1.11.9 darwin/arm64")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upstream request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("upstream error (%d): %s", resp.StatusCode, string(errBytes))
	}

	return resp, nil
}
