package agent

import (
	"bufio"
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
	return p.complete(
		ctx,
		messages,
		tools,
		nil,
	)
}

func (p *DeepSeek) StreamComplete(
	ctx context.Context,
	messages []Message,
	tools []ToolDefinition,
	onText func(string),
) (Completion, error) {
	return p.complete(
		ctx,
		messages,
		tools,
		onText,
	)
}

func (p *DeepSeek) complete(
	ctx context.Context,
	messages []Message,
	tools []ToolDefinition,
	onText func(string),
) (Completion, error) {
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
		Stream      bool             `json:"stream,omitempty"`
		Options     any              `json:"stream_options,omitempty"`
	}{Model: p.cfg.Model, Messages: messages, Tools: tools, MaxTokens: 3000, Temperature: .35}
	if onText != nil {
		payload.Stream = true
		payload.Options = map[string]bool{"include_usage": true}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Completion{}, err
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		strings.TrimRight(p.cfg.BaseURL, "/")+"/chat/completions",
		bytes.NewReader(body),
	)
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
	if onText != nil {
		return p.readStream(resp.Body, onText)
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

func (p *DeepSeek) readStream(body io.Reader, onText func(string)) (Completion, error) {
	result := Completion{Message: Message{Role: "assistant", ToolCalls: []ToolCall{}}}
	reader := bufio.NewScanner(io.LimitReader(body, 2<<20))
	reader.Buffer(make([]byte, 4096), 2<<20)
	finish := ""
	text := textStream{cfg: p.cfg, emit: onText}
	defer text.finish()
	for reader.Scan() {
		line := reader.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			if finish != "stop" && finish != "tool_calls" {
				return result, errors.New("model stream ended without a complete response")
			}
			result.Message.Content = p.cfg.redact(result.Message.Content)
			for i := range result.Message.ToolCalls {
				call := &result.Message.ToolCalls[i]
				if call.ID == "" || call.Function.Name == "" || !json.Valid([]byte(call.Function.Arguments)) {
					return result, errors.New("invalid streamed tool call")
				}
				call.Function.Arguments = p.cfg.redact(call.Function.Arguments)
			}
			return result, nil
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
					Calls   []struct {
						Index int `json:"index"`
						ToolCall
					} `json:"tool_calls"`
				} `json:"delta"`
				Finish string `json:"finish_reason"`
			} `json:"choices"`
			Usage struct {
				Total int `json:"total_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return result, errors.New("invalid model stream")
		}
		if chunk.Usage.Total > 0 {
			result.Tokens = chunk.Usage.Total
		}
		for _, choice := range chunk.Choices {
			if choice.Finish != "" {
				finish = choice.Finish
			}
			if finish == "length" {
				return result, errors.New("model output exceeded its token limit")
			}
			result.Message.Content += choice.Delta.Content
			text.push(choice.Delta.Content)
			for _, delta := range choice.Delta.Calls {
				if delta.Index < 0 || delta.Index >= 8 {
					return result, errors.New("too many streamed tool calls")
				}
				for len(result.Message.ToolCalls) <= delta.Index {
					result.Message.ToolCalls = append(result.Message.ToolCalls, ToolCall{Type: "function"})
				}
				call := &result.Message.ToolCalls[delta.Index]
				call.ID += delta.ID
				call.Function.Name += delta.Function.Name
				call.Function.Arguments += delta.Function.Arguments
			}
		}
	}
	return result, errors.New("model stream disconnected before completion")
}
