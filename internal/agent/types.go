package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

var (
	ErrNotFound = errors.New("not found")
	ErrBusy     = errors.New("a run is already active")
	ErrBudget   = errors.New("run budget exhausted")
)

type Message struct {
	Internal   bool       `json:"internal,omitempty"`
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolDefinition struct {
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function"`
}

type FunctionDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

type Completion struct {
	Message Message
	Tokens  int
}

type Model interface {
	Complete(context.Context, []Message, []ToolDefinition) (Completion, error)
}

type Event struct {
	Type string    `json:"type"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

type Session struct {
	ID        string    `json:"id"`
	Principal string    `json:"principal"`
	Messages  []Message `json:"messages"`
	ActiveRun string    `json:"active_run"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Run struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Principal string    `json:"principal"`
	Prompt    string    `json:"prompt"`
	Goal      string    `json:"goal"`
	Status    string    `json:"status"`
	Answer    string    `json:"answer"`
	Error     string    `json:"error,omitempty"`
	Events    []Event   `json:"events"`
	Sources   []Source  `json:"sources"`
	Calls     int       `json:"calls"`
	Tokens    int       `json:"tokens"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Source struct {
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	Snippet     string    `json:"snippet"`
	RetrievedAt time.Time `json:"retrieved_at"`
}

type Approval struct {
	ReceiptRecorded bool            `json:"receipt_recorded"`
	ID              string          `json:"id"`
	Principal       string          `json:"principal"`
	SessionID       string          `json:"session_id"`
	Tool            string          `json:"tool"`
	Arguments       json.RawMessage `json:"arguments"`
	Status          string          `json:"status"`
	Result          string          `json:"result,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
}

type Budget struct {
	mu        sync.Mutex
	Calls     int
	Tokens    int
	maxCalls  int
	maxTokens int
}

func (b *Budget) reserve() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.Calls >= b.maxCalls || b.Tokens >= b.maxTokens {
		return ErrBudget
	}
	b.Calls++
	return nil
}

func (b *Budget) add(tokens int)    { b.mu.Lock(); b.Tokens += tokens; b.mu.Unlock() }
func (b *Budget) usage() (int, int) { b.mu.Lock(); defer b.mu.Unlock(); return b.Calls, b.Tokens }

func newID() string {
	var value [24]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic("secure random unavailable")
	}
	return hex.EncodeToString(value[:])
}

func clip(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "\n[truncated]"
}
