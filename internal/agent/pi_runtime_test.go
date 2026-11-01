package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestInvalidToolArgumentsCannotReachHooksOrApproval(t *testing.T) {
	s := reliabilityService(t, nil)
	var hooks atomic.Int32
	s.hooks = []Hook{func(context.Context, HookEvent) error { hooks.Add(1); return nil }}
	checks := []struct {
		tool string
		args string
	}{
		{tool: "profile_read", args: `{"query":"belongs to another tool"}`},
		{tool: "web_search", args: `{}`},
		{tool: "web_search", args: `{"query":null}`},
		{tool: "web_search", args: `{"query":42}`},
		{tool: "web_search", args: `{"query":"x"} {}`},
		{tool: "memory_write", args: `{"title":"x"}`},
		{tool: "mcp_call", args: `{"name":"x","agent":"y","arguments":[]}`},
		{tool: "team_create", args: `{"name":"x","members":[null],"content":"y"}`},
		{tool: "todo_write", args: `{"items":[{"content":"x","status":"unknown"}]}`},
		{tool: "todo_write", args: `{"items":[{"content":"x","status":"pending","surprise":true}]}`},
		{tool: "profile_read", args: `null`},
	}
	for _, check := range checks {
		result := s.execute(
			context.Background(),
			&Execution{Principal: "owner", Owner: true, Agent: "lead"},
			ToolCall{Function: FunctionCall{Name: check.tool, Arguments: check.args}},
			false,
		)
		var receipt map[string]any
		if json.Unmarshal([]byte(result), &receipt) != nil || receipt["error"] == nil {
			t.Errorf("invalid %s arguments accepted: %s", check.tool, result)
		}
	}
	approvals, err := records[Approval](s.store, "approvals")
	if err != nil || len(approvals) != 0 || hooks.Load() != 0 {
		t.Fatalf("invalid input reached a hook or side effect: approvals=%d hooks=%d err=%v", len(approvals), hooks.Load(), err)
	}
}

type gatedSearch struct {
	started chan string
	release map[string]chan struct{}
	active  atomic.Int32
	maximum atomic.Int32
	before  func(string)
}

func (s *gatedSearch) Search(ctx context.Context, query string) ([]Source, error) {
	active := s.active.Add(1)
	defer s.active.Add(-1)
	for previous := s.maximum.Load(); active > previous; previous = s.maximum.Load() {
		if s.maximum.CompareAndSwap(previous, active) {
			break
		}
	}
	if s.before != nil {
		s.before(query)
	}
	s.started <- query
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.release[query]:
	}
	return []Source{{Title: query, URL: "https://example.com/" + query}}, nil
}

func (*gatedSearch) Read(context.Context, string) (string, error) { panic("test tool panic") }

func testRun(t *testing.T, s *Service) Run {
	t.Helper()
	run := Run{
		ID: newID(), Principal: "visitor", Status: "running", Phase: "thinking",
		Events: []Event{}, Inputs: []RunInput{}, PendingTools: []string{},
	}
	if err := s.store.put("runs", run.ID, run); err != nil {
		t.Fatal(err)
	}
	return run
}

func searchCall(id string) ToolCall {
	return ToolCall{ID: id, Type: "function", Function: FunctionCall{
		Name: "web_search", Arguments: toolJSON(map[string]string{"query": id}),
	}}
}

func take[T any](t *testing.T, input <-chan T) T {
	t.Helper()
	select {
	case value := <-input:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the agent")
		var zero T
		return zero
	}
}

func TestParallelReadToolsBoundConcurrencyAndPreserveReceiptOrder(t *testing.T) {
	cfg := reliabilityConfig(t)
	cfg.ToolConcurrency = 2
	s, err := NewService(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	web := &gatedSearch{
		started: make(chan string, 3),
		release: map[string]chan struct{}{"one": make(chan struct{}), "two": make(chan struct{}), "three": make(chan struct{})},
	}
	s.web = web
	run := testRun(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan []Message, 1)
	go func() {
		finished <- s.executeBatch(ctx, &Execution{RunID: run.ID, Agent: "lead"}, []ToolCall{
			searchCall("one"), searchCall("two"), searchCall("three"),
		})
	}()
	first, second := take(t, web.started), take(t, web.started)
	select {
	case <-web.started:
		t.Fatal("tool concurrency limit was exceeded")
	case <-time.After(30 * time.Millisecond):
	}
	close(web.release[second])
	third := take(t, web.started)
	close(web.release[third])
	close(web.release[first])
	results := take(t, finished)
	for i, id := range []string{"one", "two", "three"} {
		if results[i].ToolCallID != id || !strings.Contains(results[i].Content, id) {
			t.Fatalf("model receipts lost source order: %+v", results)
		}
	}
	if err := s.store.get("runs", run.ID, &run); err != nil {
		t.Fatal(err)
	}
	endOrder := []string{}
	for _, event := range run.Events {
		if event.Type == "tool_end" {
			endOrder = append(endOrder, event.CallID)
		}
	}
	if len(endOrder) != 3 || endOrder[0] != second || web.maximum.Load() != 2 || len(run.PendingTools) != 0 {
		t.Fatalf("completion order/state incorrect: %v maximum=%d pending=%v", endOrder, web.maximum.Load(), run.PendingTools)
	}
}

func TestStateChangingToolMakesWholeBatchSequential(t *testing.T) {
	s := reliabilityService(t, nil)
	before, after := make(chan struct{}), make(chan struct{})
	close(after)
	web := &gatedSearch{
		started: make(chan string, 2), release: map[string]chan struct{}{"before": before, "after": after},
		before: func(query string) {
			if query != "after" {
				return
			}
			var plan TodoList
			if err := s.store.get("todos", "session", &plan); err != nil || len(plan.Items) != 1 {
				t.Error("later read ran before the earlier state-changing call")
			}
		},
	}
	s.web = web
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan []Message, 1)
	go func() {
		finished <- s.executeBatch(ctx, &Execution{SessionID: "session", Agent: "lead"}, []ToolCall{
			searchCall("before"),
			{ID: "plan", Function: FunctionCall{
				Name: "todo_write", Arguments: `{"items":[{"content":"evidence","status":"completed"}]}`,
			}},
			searchCall("after"),
		})
	}()
	if take(t, web.started) != "before" {
		t.Fatal("batch did not preserve call order")
	}
	close(before)
	if take(t, web.started) != "after" || len(take(t, finished)) != 3 || web.maximum.Load() != 1 {
		t.Fatal("state-changing batch ran in parallel")
	}
}

func TestToolPanicAndCancellationBecomeReceipts(t *testing.T) {
	s := reliabilityService(t, nil)
	s.web = &gatedSearch{}
	run := testRun(t, s)
	ex := &Execution{RunID: run.ID, Agent: "lead"}
	call := ToolCall{ID: "panic", Function: FunctionCall{Name: "web_read", Arguments: `{"url":"https://example.com"}`}}
	result := s.execute(context.Background(), ex, call, false)
	if !strings.Contains(result, "tool execution panicked") {
		t.Fatalf("tool panic escaped the executor: %s", result)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.execute(ctx, ex, searchCall("cancel"), false)
	if err := s.store.get("runs", run.ID, &run); err != nil {
		t.Fatal(err)
	}
	last := run.Events[len(run.Events)-1]
	if last.Status != "cancelled" || len(run.PendingTools) != 0 {
		t.Fatalf("cancelled tool did not settle: %+v", last)
	}
}

type controlledModel struct {
	requests chan []Message
	replies  chan Completion
}

func (m *controlledModel) Complete(ctx context.Context, messages []Message, _ []ToolDefinition) (Completion, error) {
	select {
	case m.requests <- append([]Message{}, messages...):
	case <-ctx.Done():
		return Completion{}, ctx.Err()
	}
	select {
	case reply := <-m.replies:
		return reply, nil
	case <-ctx.Done():
		return Completion{}, ctx.Err()
	}
}

func settledRun(t *testing.T, s *Service, id string) Run {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		var run Run
		if err := s.store.get("runs", id, &run); err != nil {
			t.Fatal(err)
		}
		if run.Status != "running" {
			return run
		}
		select {
		case <-deadline:
			t.Fatal("run never settled")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestSteeringPrecedesNextRequestAndFollowUpWaitsForNaturalEnd(t *testing.T) {
	model := &controlledModel{requests: make(chan []Message, 4), replies: make(chan Completion)}
	s := reliabilityService(t, model)
	session, _ := s.createSession("visitor")
	run, err := s.Start("visitor", session.ID, ChatInput{Message: "begin"}, false)
	if err != nil {
		t.Fatal(err)
	}
	take(t, model.requests)
	for _, input := range []QueueInput{
		{ID: "follow", Mode: "follow_up", Message: "later"},
		{ID: "steer", Mode: "steer", Message: "correct direction"},
	} {
		if _, err := s.Queue("visitor", run.ID, input); err != nil {
			t.Fatal(err)
		}
	}
	model.replies <- Completion{Message: Message{Role: "assistant", ToolCalls: []ToolCall{{
		ID: "profile", Function: FunctionCall{Name: "profile_read", Arguments: "{}"},
	}}}}
	second := take(t, model.requests)
	if second[len(second)-1].Content != "correct direction" || second[len(second)-2].Role != "tool" {
		t.Fatalf("steering split a tool/result pair or was not injected: %+v", second)
	}
	for _, message := range second {
		if message.Content == "later" {
			t.Fatal("follow-up interrupted the current task")
		}
	}
	model.replies <- Completion{Message: Message{Role: "assistant", Content: "first answer"}}
	third := take(t, model.requests)
	if third[len(third)-1].Content != "later" || third[len(third)-2].Content != "first answer" {
		t.Fatal("follow-up did not continue after the natural end")
	}
	model.replies <- Completion{Message: Message{Role: "assistant", Content: "final answer"}}
	run = settledRun(t, s, run.ID)
	if run.Status != "completed" || run.Answer != "final answer" || run.Turns != 3 || run.Phase != "completed" {
		t.Fatalf("bad final state: %+v", run)
	}
	starts, ends := 0, 0
	for _, event := range run.Events {
		if event.Type == "turn_start" {
			starts++
		}
		if event.Type == "turn_end" {
			ends++
		}
	}
	if starts != 3 || ends != 3 || run.Events[len(run.Events)-1].Type != "agent_end" {
		t.Fatalf("lifecycle is not balanced: starts=%d ends=%d", starts, ends)
	}
	for _, input := range run.Inputs {
		if input.Status != "delivered" {
			t.Fatal("accepted input was not consumed")
		}
	}
}

func TestQueueIdempotenceRemovalAndAtomicFinish(t *testing.T) {
	s := reliabilityService(t, nil)
	run := testRun(t, s)
	input := QueueInput{ID: "same", Mode: "steer", Message: "one message"}
	if _, err := s.Queue("visitor", run.ID, input); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Queue("visitor", run.ID, input); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Queue("other", run.ID, input); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign visitor accessed the queue")
	}
	if err := s.RemoveInput("visitor", run.ID, input.ID); err != nil {
		t.Fatal(err)
	}
	accepted := make(chan string, 24)
	seed, err := s.Queue("visitor", run.ID, QueueInput{Mode: "follow_up", Message: "accepted before finishing"})
	if err != nil {
		t.Fatal(err)
	}
	accepted <- seed.ID
	var writers sync.WaitGroup
	for range 23 {
		writers.Add(1)
		go func() {
			defer writers.Done()
			item, err := s.Queue("visitor", run.ID, QueueInput{Mode: "follow_up", Message: "queued"})
			if err == nil {
				accepted <- item.ID
			} else if !errors.Is(err, errRunSettling) {
				t.Errorf("unexpected queue error: %v", err)
			}
		}()
	}
	ex := &Execution{RunID: run.ID}
	for {
		item, err := s.nextInput(ex, true)
		if err != nil {
			t.Fatal(err)
		}
		if item == nil {
			break
		}
	}
	writers.Wait()
	close(accepted)
	if err := s.store.get("runs", run.ID, &run); err != nil {
		t.Fatal(err)
	}
	if run.Inputs[0].Status != "removed" || run.Phase != "finishing" {
		t.Fatal("queue removal or finish seal was lost")
	}
	for id := range accepted {
		found := false
		for _, item := range run.Inputs {
			if item.ID == id && item.Status == "delivered" {
				found = true
			}
		}
		if !found {
			t.Fatal("a message accepted during finishing was lost")
		}
	}
	queued := 0
	for _, event := range run.Events {
		if event.InputID == "same" && event.Type == "input_queued" {
			queued++
		}
	}
	if queued != 1 {
		t.Fatal("idempotent retry duplicated a queued message")
	}
}

func TestCancelPreservesUnprocessedQueueAndLifecycle(t *testing.T) {
	model := &controlledModel{requests: make(chan []Message, 1), replies: make(chan Completion)}
	s := reliabilityService(t, model)
	session, _ := s.createSession("visitor")
	run, err := s.Start("visitor", session.ID, ChatInput{Message: "start"}, false)
	if err != nil {
		t.Fatal(err)
	}
	take(t, model.requests)
	if _, err := s.Queue("visitor", run.ID, QueueInput{Mode: "follow_up", Message: "do not lose this"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Cancel("visitor", run.ID); err != nil {
		t.Fatal(err)
	}
	run = settledRun(t, s, run.ID)
	if run.Status != "cancelled" || run.Inputs[0].Status != "unprocessed" || run.Events[len(run.Events)-1].Type != "agent_end" {
		t.Fatalf("cancel lost queue or lifecycle: %+v", run)
	}
}

func TestQueueHTTPUsesRunIdentityAndValidatesMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := reliabilityService(t, nil)
	run := testRun(t, s)
	run.Principal = newID()
	if err := s.store.put("runs", run.ID, run); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	s.Register(router)
	for _, check := range []struct {
		principal string
		body      string
		want      int
	}{
		{principal: newID(), body: `{"mode":"steer","message":"x"}`, want: 404},
		{principal: run.Principal, body: `{"mode":"bad","message":"x"}`, want: 400},
		{principal: run.Principal, body: `{"id":"http","mode":"steer","message":"x"}`, want: 202},
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/runs/"+run.ID+"/inputs", strings.NewReader(check.body))
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(&http.Cookie{Name: "durlim_agent", Value: check.principal})
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != check.want {
			t.Fatalf("HTTP queue status %d: %s", recorder.Code, recorder.Body.String())
		}
	}
}

func TestRestartRetainsQueuedInputsWithoutReplaying(t *testing.T) {
	cfg := reliabilityConfig(t)
	s, err := NewService(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	run := testRun(t, s)
	run.Inputs = []RunInput{{ID: "pending", Content: "retain me", Mode: "follow_up", Status: "pending"}}
	run.PendingTools = []string{"unknown-outcome"}
	if err := s.store.put("runs", run.ID, run); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewService(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	if err := restarted.store.get("runs", run.ID, &run); err != nil {
		t.Fatal(err)
	}
	if run.Status != "interrupted" || run.Phase != "interrupted" || len(run.PendingTools) != 0 {
		t.Fatalf("runtime state was not recovered: %+v", run)
	}
	if run.Inputs[0].Status != "unprocessed" || run.Inputs[0].Content != "retain me" {
		t.Fatal("restart discarded or replayed queued input")
	}
	if run.Events[len(run.Events)-1].Type != "agent_end" {
		t.Fatal("restart did not finalize the run lifecycle")
	}
}

func TestPostToolHookCannotEraseActualSideEffectReceipt(t *testing.T) {
	s := reliabilityService(t, nil)
	run := testRun(t, s)
	s.hooks = []Hook{func(_ context.Context, event HookEvent) error {
		if event.Phase == "PostToolUse" {
			if event.Result == "" || event.Status != "completed" {
				t.Error("post hook did not receive the structured actual result")
			}
			return errors.New("observer failed")
		}
		return nil
	}}
	result := s.execute(
		context.Background(),
		&Execution{RunID: run.ID, SessionID: "session", Agent: "lead"},
		ToolCall{ID: "plan", Function: FunctionCall{
			Name: "todo_write", Arguments: `{"items":[{"content":"done","status":"completed"}]}`,
		}},
		false,
	)
	if !strings.Contains(result, "done") || strings.Contains(result, "observer failed") {
		t.Fatalf("observation failure replaced the actual receipt: %s", result)
	}
	var plan TodoList
	if err := s.store.get("todos", "session", &plan); err != nil || len(plan.Items) != 1 {
		t.Fatal("side effect did not execute")
	}
	if err := s.store.get("runs", run.ID, &run); err != nil {
		t.Fatal(err)
	}
	if run.Events[len(run.Events)-1].Status != "completed" {
		t.Fatal("hook warning changed completed tool status")
	}
}

func TestDuplicateModelToolIDsFailBeforeAnyToolRuns(t *testing.T) {
	model := &controlledModel{requests: make(chan []Message, 1), replies: make(chan Completion)}
	s := reliabilityService(t, model)
	web := &gatedSearch{}
	s.web = web
	session, _ := s.createSession("visitor")
	run, err := s.Start("visitor", session.ID, ChatInput{Message: "test tool batch"}, false)
	if err != nil {
		t.Fatal(err)
	}
	take(t, model.requests)
	model.replies <- Completion{Message: Message{Role: "assistant", ToolCalls: []ToolCall{
		searchCall("duplicate"), searchCall("duplicate"),
	}}}
	run = settledRun(t, s, run.ID)
	if run.Status != "failed" || !strings.Contains(run.Error, "duplicate tool call id") || web.maximum.Load() != 0 {
		t.Fatalf("malformed batch reached execution: %+v", run)
	}
}
