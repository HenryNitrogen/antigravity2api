package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
)

const (
	V1InternalURL      = "https://daily-cloudcode-pa.googleapis.com/v1internal"
	V1InternalFallback = "https://cloudcode-pa.googleapis.com/v1internal"
)

// FetchProjectID calls loadCodeAssist to retrieve the companion project ID
func FetchProjectID(ctx context.Context, accessToken string, httpClient *http.Client) (string, error) {
	url := V1InternalURL + ":loadCodeAssist"
	payload := map[string]any{
		"metadata": map[string]string{
			"ideType": "ANTIGRAVITY",
		},
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", "antigravity/1.11.9 darwin/arm64")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("loadCodeAssist returned status %d", resp.StatusCode)
	}

	var res struct {
		CloudAICompanionProject string `json:"cloudaicompanionProject"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}

	if res.CloudAICompanionProject != "" {
		return res.CloudAICompanionProject, nil
	}
	return GenerateRandomProjectID(), nil
}

// GenerateRandomProjectID generates a fallback project ID like 'fabled-setup-3dmkj'
func GenerateRandomProjectID() string {
	adjectives := []string{
		"fabled", "spry", "apt", "astral", "infra", "brisk", "calm", "daring",
		"eager", "gentle", "lively", "noble", "quick", "rural", "solar", "tidy",
		"vivid", "witty", "young", "zesty",
	}
	nouns := []string{
		"setup", "post", "site", "scout", "battery", "arbor", "beacon", "canyon",
		"delta", "ember", "grove", "harbor", "meadow", "nexus", "prairie", "ridge",
		"savanna", "tundra", "valley", "willow",
	}

	adjIdx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(adjectives))))
	nounIdx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(nouns))))

	charset := "abcdefghijklmnopqrstuvwxyz0123456789"
	suffix := make([]byte, 5)
	for i := range suffix {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		suffix[i] = charset[n.Int64()]
	}

	return fmt.Sprintf("%s-%s-%s", adjectives[adjIdx.Int64()], nouns[nounIdx.Int64()], string(suffix))
}
