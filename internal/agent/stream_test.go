package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type streamFixture struct {
	release chan struct{}
}

func (m *streamFixture) Complete(context.Context, []Message, []ToolDefinition) (Completion, error) {
	panic("conversational requests must stream")
}

func (m *streamFixture) StreamComplete(
	ctx context.Context,
	messages []Message,
	_ []ToolDefinition,
	onText func(string),
) (Completion, error) {
	if messages[len(messages)-1].Role != "tool" {
		onText("我先读取资料。")
		return Completion{Message: Message{
			Role: "assistant", Content: "我先读取资料。",
			ToolCalls: []ToolCall{{
				ID: "profile-call", Type: "function",
				Function: FunctionCall{Name: "profile_read", Arguments: "{}"},
			}},
		}, Tokens: 8}, nil
	}
	onText("## 概览\n\n")
	select {
	case <-ctx.Done():
		return Completion{}, ctx.Err()
	case <-m.release:
	}
	onText("**实时**回复。")
	return Completion{Message: Message{Role: "assistant", Content: "## 概览\n\n**实时**回复。"}, Tokens: 12}, nil
}

func TestSSEStreamsBeforeCompletionAndReplaysCursor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	model := &streamFixture{release: make(chan struct{})}
	s := reliabilityService(t, model)
	principal := newID()
	session, err := s.createSession(principal)
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.Start(principal, session.ID, ChatInput{Message: "请读取资料"}, false)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	s.Register(router)
	server := httptest.NewServer(router)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/agent/runs/"+run.ID+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: "durlim_agent", Value: principal})
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.Header.Get("X-Accel-Buffering") != "no" || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("streaming headers missing: %v", response.Header)
	}
	reader := bufio.NewScanner(response.Body)
	cursor := ""
	sawToolStart, sawToolEnd, released, completed := false, false, false, false
	for reader.Scan() {
		line := reader.Text()
		if strings.HasPrefix(line, "id: ") {
			cursor = strings.TrimPrefix(line, "id: ")
		}
		if line == "event: done" {
			completed = true
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event Event
		_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event)
		switch event.Type {
		case "tool_start":
			sawToolStart = event.CallID == "profile-call" && event.Arguments == "{}"
		case "tool_end":
			sawToolEnd = event.Status == "completed" && event.Result != ""
		case "assistant_delta":
			if !released && strings.Contains(event.Text, "概览") {
				var current Run
				if err := s.store.get("runs", run.ID, &current); err != nil || current.Status != "running" {
					t.Fatal("text was buffered until the run ended")
				}
				released = true
				close(model.release)
			}
		}
	}
	if !sawToolStart || !sawToolEnd || !released || !completed || reader.Err() != nil {
		t.Fatalf("incomplete event sequence: start=%v end=%v live=%v done=%v err=%v",
			sawToolStart, sawToolEnd, released, completed, reader.Err())
	}
	var saved Run
	if err := s.store.get("runs", run.ID, &saved); err != nil || saved.Tokens != 20 {
		t.Fatalf("usage not saved: %+v %v", saved, err)
	}

	for _, check := range []struct {
		name      string
		principal string
		cursor    string
		want      int
	}{
		{name: "replay", principal: principal, cursor: cursor, want: 200},
		{name: "foreign", principal: newID(), want: 404},
		{name: "bad_cursor", principal: principal, cursor: "999999", want: 400},
	} {
		t.Run(check.name, func(t *testing.T) {
			replay := httptest.NewRequest(http.MethodGet, "/api/v1/agent/runs/"+run.ID+"/events", nil)
			replay.AddCookie(&http.Cookie{Name: "durlim_agent", Value: check.principal})
			replay.Header.Set("Last-Event-ID", check.cursor)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, replay)
			if recorder.Code != check.want {
				t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
			}
			if check.name == "replay" {
				if strings.Contains(recorder.Body.String(), "event: activity") || !strings.Contains(recorder.Body.String(), "event: done") {
					t.Fatal("resume duplicated events or lost terminal state")
				}
			}
		})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.watchers) != 0 {
		t.Fatal("completed streams leaked subscriptions")
	}
}

func TestToolEventsIncludeEarlyFailureAndApproval(t *testing.T) {
	s := reliabilityService(t, nil)
	session, _ := s.createSession("visitor")
	run := Run{ID: newID(), SessionID: session.ID, Principal: "visitor", Events: []Event{}}
	if err := s.store.put("runs", run.ID, run); err != nil {
		t.Fatal(err)
	}
	ex := &Execution{RunID: run.ID, SessionID: session.ID, Principal: "visitor", Agent: "lead"}
	for _, check := range []struct {
		name   string
		args   string
		status string
	}{
		{name: "unknown", args: "{}", status: "failed"},
		{name: "profile_read", args: "{", status: "failed"},
		{name: "memory_write", args: `{"title":"x","content":"y"}`, status: "approval_required"},
	} {
		s.execute(context.Background(), ex, ToolCall{Function: FunctionCall{Name: check.name, Arguments: check.args}}, false)
		if err := s.store.get("runs", run.ID, &run); err != nil {
			t.Fatal(err)
		}
		end := run.Events[len(run.Events)-1]
		if end.Type != "tool_end" || end.Status != check.status || end.CallID == "" || end.Result == "" {
			t.Fatalf("tool outcome lost: %+v", end)
		}
	}
}

func TestSSEDisconnectDoesNotCancelRunAndExplicitCancelKeepsPartialText(t *testing.T) {
	s := reliabilityService(t, &streamFixture{release: make(chan struct{})})
	principal := newID()
	session, err := s.createSession(principal)
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.Start(principal, session.ID, ChatInput{Message: "test cancellation"}, false)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	s.Register(router)
	server := httptest.NewServer(router)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		server.URL+"/api/v1/agent/runs/"+run.ID+"/events",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(&http.Cookie{Name: "durlim_agent", Value: principal})
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewScanner(response.Body)
	sawPartial := false
	for reader.Scan() {
		if strings.Contains(reader.Text(), "概览") {
			sawPartial = true
			break
		}
	}
	_ = response.Body.Close()
	if !sawPartial {
		t.Fatal("did not receive partial response")
	}
	if err := s.store.get("runs", run.ID, &run); err != nil || run.Status != "running" {
		t.Fatalf("disconnect cancelled durable run: %+v %v", run, err)
	}
	if err := s.Cancel(principal, run.ID); err != nil {
		t.Fatal(err)
	}
	for run.Status == "running" {
		select {
		case <-ctx.Done():
			t.Fatal("cancel did not settle run")
		case <-time.After(5 * time.Millisecond):
		}
		if err := s.store.get("runs", run.ID, &run); err != nil {
			t.Fatal(err)
		}
	}
	if run.Status != "cancelled" || run.Answer != "" {
		t.Fatalf("cancelled output reported as complete: %+v", run)
	}
	kept := false
	for _, event := range run.Events {
		if event.Type == "assistant_delta" && strings.Contains(event.Text, "概览") {
			kept = true
		}
	}
	if !kept {
		t.Fatal("cancel discarded already-delivered text")
	}
}

func TestStreamRedactsSecretsAcrossEveryChunkBoundary(t *testing.T) {
	cfg := Config{APIKey: "a-provider-credential", OwnerToken: "owner-credential"}
	for _, secret := range []string{cfg.APIKey, cfg.OwnerToken, "sk-abcdefghijklmnop", "SK-1234567890abcdef"} {
		for split := 1; split < len(secret); split++ {
			var output strings.Builder
			stream := textStream{cfg: cfg, emit: func(part string) { output.WriteString(part) }}
			stream.push("before " + secret[:split])
			stream.push(secret[split:] + " after")
			stream.finish()
			if output.String() != "before [REDACTED] after" {
				t.Fatalf("secret leaked at split %d: %q", split, output.String())
			}
		}
	}
}

func TestProviderStreamMergesToolArgumentsAndUsage(t *testing.T) {
	frames := []string{
		`{"choices":[{"delta":{"content":"你好","reasoning_content":"private reasoning"}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"web_search","arguments":"{\"query\":"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"Go\"}"}}]},"finish_reason":"tool_calls"}]}`,
		`{"choices":[],"usage":{"total_tokens":42}}`,
		`[DONE]`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Stream  bool `json:"stream"`
			Options struct {
				Usage bool `json:"include_usage"`
			} `json:"stream_options"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || !payload.Stream || !payload.Options.Usage {
			t.Error("provider request did not enable real streaming and usage")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, frame := range frames {
			_, _ = io.WriteString(w, "data: "+frame+"\n\n")
		}
	}))
	defer server.Close()
	p := &DeepSeek{cfg: Config{BaseURL: server.URL}, client: server.Client()}
	var text strings.Builder
	result, err := p.StreamComplete(context.Background(), []Message{}, []ToolDefinition{}, func(part string) { text.WriteString(part) })
	if err != nil || result.Tokens != 42 || text.String() != "你好" || len(result.Message.ToolCalls) != 1 {
		t.Fatalf("invalid completion: %+v text=%q err=%v", result, text.String(), err)
	}
	call := result.Message.ToolCalls[0]
	if call.ID != "call-1" || call.Function.Name != "web_search" || call.Function.Arguments != `{"query":"Go"}` {
		t.Fatalf("fragmented tool call was not assembled: %+v", call)
	}
	for _, bad := range []string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n",
		"data: {\"choices\":[{\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n",
		"data: broken\n\n",
	} {
		if _, err := p.readStream(strings.NewReader(bad), func(string) {}); err == nil {
			t.Fatal("truncated or malformed provider stream reported success")
		}
	}
}
