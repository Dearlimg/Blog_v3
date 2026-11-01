package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type DeepSeek struct {
	cfg    Config
	client *http.Client
}

func NewDeepSeek(cfg Config) (*DeepSeek, error) {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Scheme != "https" || u.Hostname() != "api.deepseek.com" || u.User != nil {
		return nil, errors.New("DeepSeek endpoint must use https://api.deepseek.com")
	}
	return &DeepSeek{cfg: cfg, client: &http.Client{
		Timeout:       90 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (p *DeepSeek) Complete(ctx context.Context, messages []Message, tools []ToolDefinition) (Completion, error) {
	// Internal receipts belong in model context, but storage-only flags do not.
	messages = append([]Message{}, messages...)
	for i := range messages {
		messages[i].Internal = false
	}
	payload := struct {
		Model       string           `json:"model"`
		Messages    []Message        `json:"messages"`
		Tools       []ToolDefinition `json:"tools,omitempty"`
		MaxTokens   int              `json:"max_tokens"`
		Temperature float64          `json:"temperature"`
	}{Model: p.cfg.Model, Messages: messages, Tools: tools, MaxTokens: 3000, Temperature: .35}
	body, err := json.Marshal(payload)
	if err != nil {
		return Completion{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(p.cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Completion{}, errors.New("cannot build model request")
	}
	req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return Completion{}, errors.New("model connection failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		// Provider error bodies can contain credentials or prompt data. Never relay them.
		return Completion{}, fmt.Errorf("model service returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		Choices []struct {
			Message Message `json:"message"`
			Finish  string  `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&result); err != nil {
		return Completion{}, errors.New("invalid model response")
	}
	if len(result.Choices) != 1 {
		return Completion{}, errors.New("missing model response")
	}
	choice := result.Choices[0]
	if choice.Finish == "length" {
		return Completion{}, errors.New("model output exceeded its token limit")
	}
	choice.Message.Role = "assistant"
	choice.Message.Internal = false
	choice.Message.Content = p.cfg.redact(choice.Message.Content)
	for i := range choice.Message.ToolCalls {
		choice.Message.ToolCalls[i].Function.Arguments = p.cfg.redact(choice.Message.ToolCalls[i].Function.Arguments)
	}
	return Completion{Message: choice.Message, Tokens: result.Usage.TotalTokens}, nil
}
