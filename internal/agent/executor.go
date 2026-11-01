package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

type toolOutcome struct {
	Content string
	Status  string
}

type preparedTool struct {
	Call      ToolCall
	Tool      Tool
	Args      Args
	StartedAt time.Time
	Immediate *toolOutcome
}

func failedTool(err error) toolOutcome {
	return toolOutcome{Content: toolJSON(map[string]string{"error": err.Error()}), Status: "failed"}
}

// validateArguments uses exactly the schema advertised to the model. Decoding
// every tool into Args alone would accept fields belonging to a different tool.
func validateArguments(tool Tool, raw string) (Args, error) {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(raw), &fields); err != nil || fields == nil {
		return Args{}, errors.New("tool arguments must be a single JSON object")
	}
	for _, required := range tool.Required {
		if _, ok := fields[required]; !ok {
			return Args{}, fmt.Errorf("missing required argument %q", required)
		}
	}
	for name, value := range fields {
		kind, allowed := tool.Fields[name]
		if !allowed {
			return Args{}, fmt.Errorf("argument %q is not allowed for this tool", name)
		}
		if string(value) == "null" {
			return Args{}, fmt.Errorf("argument %q cannot be null", name)
		}
		var err error
		switch kind {
		case "string":
			var text string
			err = json.Unmarshal(value, &text)
		case "integer":
			var number int
			err = json.Unmarshal(value, &number)
		case "object":
			object := map[string]json.RawMessage{}
			err = json.Unmarshal(value, &object)
		case "strings":
			items := []json.RawMessage{}
			err = json.Unmarshal(value, &items)
			for _, item := range items {
				var text string
				if string(item) == "null" || json.Unmarshal(item, &text) != nil {
					err = errors.New("expected an array of strings")
					break
				}
			}
		case "todos":
			items := []json.RawMessage{}
			err = json.Unmarshal(value, &items)
			for _, item := range items {
				_, itemErr := validateArguments(Tool{
					Fields:   map[string]string{"content": "string", "status": "string"},
					Required: []string{"content", "status"},
				}, string(item))
				var todo Todo
				if itemErr != nil || json.Unmarshal(item, &todo) != nil {
					err = errors.New("todo items require content and status strings")
					break
				}
				switch todo.Status {
				case "pending", "in_progress", "completed":
				default:
					err = errors.New("invalid todo status")
				}
			}
		default:
			err = errors.New("unsupported argument type")
		}
		if err != nil {
			return Args{}, fmt.Errorf("invalid %s argument %q", kind, name)
		}
	}
	var args Args
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return Args{}, errors.New("invalid tool arguments")
	}
	return args, nil
}

func (s *Service) prepareTool(ctx context.Context, ex *Execution, call ToolCall) preparedTool {
	if call.ID == "" {
		call.ID = newID()
	}
	prepared := preparedTool{Call: call, StartedAt: time.Now()}
	_ = s.recordEvent(ex.RunID, Event{
		Type: "tool_start", Agent: ex.Agent, Tool: call.Function.Name, CallID: call.ID,
		Arguments: call.Function.Arguments, Status: "preparing", Turn: ex.Turn,
	})
	if err := ctx.Err(); err != nil {
		outcome := failedTool(err)
		outcome.Status = "cancelled"
		prepared.Immediate = &outcome
		return prepared
	}
	tool, ok := s.tools[call.Function.Name]
	var err error
	switch {
	case !ok:
		err = errors.New("unknown tool")
	case tool.Owner && !ex.Owner:
		err = errors.New("owner permission required")
	case tool.LeadOnly && ex.Depth > 0:
		err = errors.New("lead-only tool")
	case call.Type != "" && call.Type != "function":
		err = errors.New("unsupported tool call type")
	default:
		prepared.Tool = tool
		prepared.Args, err = validateArguments(tool, call.Function.Arguments)
	}
	if err == nil {
		err = s.hook(ctx, HookEvent{
			Phase: "PreToolUse", RunID: ex.RunID, Tool: call.Function.Name,
			CallID: call.ID, Agent: ex.Agent, Arguments: call.Function.Arguments,
		})
	}
	if err != nil {
		outcome := failedTool(err)
		prepared.Immediate = &outcome
	}
	return prepared
}

func (s *Service) execute(
	ctx context.Context,
	ex *Execution,
	call ToolCall,
	approved bool,
) string {
	prepared := s.prepareTool(ctx, ex, call)
	return s.runPreparedTool(
		ctx,
		ex,
		prepared,
		approved,
	)
}

func (s *Service) runPreparedTool(
	ctx context.Context,
	ex *Execution,
	prepared preparedTool,
	approved bool,
) string {
	outcome := failedTool(errors.New("tool did not execute"))
	// A tool panic becomes a failed receipt instead of terminating a worker/process.
	func() {
		defer func() {
			if recover() != nil {
				outcome = failedTool(errors.New("tool execution panicked"))
			}
		}()
		outcome = s.invokeTool(
			ctx,
			ex,
			prepared,
			approved,
		)
	}()
	outcome.Content = s.cfg.redact(outcome.Content)
	if prepared.Immediate == nil {
		err := s.hook(ctx, HookEvent{
			Phase: "PostToolUse", RunID: ex.RunID, Agent: ex.Agent,
			Tool: prepared.Call.Function.Name, CallID: prepared.Call.ID,
			Arguments: prepared.Call.Function.Arguments, Result: outcome.Content, Status: outcome.Status,
		})
		if err != nil {
			// Observation cannot undo an external side effect or replace its receipt.
			_ = s.recordEvent(ex.RunID, Event{
				Type: "HookWarning", Tool: prepared.Call.Function.Name, CallID: prepared.Call.ID,
				Text: "Post-tool hook failed; the actual tool outcome is preserved.",
			})
		}
	}
	_ = s.recordEvent(ex.RunID, Event{
		Type: "tool_end", Agent: ex.Agent, Tool: prepared.Call.Function.Name, CallID: prepared.Call.ID,
		Status: outcome.Status, Result: outcome.Content, Turn: ex.Turn,
		DurationMS: time.Since(prepared.StartedAt).Milliseconds(),
	})
	return outcome.Content
}

func (s *Service) invokeTool(
	ctx context.Context,
	ex *Execution,
	prepared preparedTool,
	approved bool,
) toolOutcome {
	if prepared.Immediate != nil {
		return *prepared.Immediate
	}
	if prepared.Tool.Parallel {
		select {
		case s.toolSlots <- struct{}{}:
			defer func() { <-s.toolSlots }()
		case <-ctx.Done():
			return toolOutcome{
				Content: toolJSON(map[string]string{"error": "tool cancelled before execution"}), Status: "cancelled",
			}
		}
	}
	if ctx.Err() != nil {
		return toolOutcome{
			Content: toolJSON(map[string]string{"error": "tool cancelled before execution"}), Status: "cancelled",
		}
	}
	name := prepared.Call.Function.Name
	if prepared.Tool.Approval && !approved {
		approval := Approval{
			ID: newID(), Principal: ex.Principal, SessionID: ex.SessionID, Tool: name,
			Arguments: json.RawMessage(s.cfg.redact(prepared.Call.Function.Arguments)),
			Status:    "pending", CreatedAt: time.Now().UTC(),
		}
		if err := s.store.put("approvals", approval.ID, approval); err != nil {
			return failedTool(errors.New("cannot store approval"))
		}
		return toolOutcome{Status: "approval_required", Content: toolJSON(map[string]string{
			"status": "approval_required", "approval_id": approval.ID,
			"message": "The user must approve this exact action in the UI. It has not executed.",
		})}
	}
	_ = s.recordEvent(ex.RunID, Event{
		Type: "tool_update", Tool: name, Agent: ex.Agent, CallID: prepared.Call.ID,
		Status: "running", Text: "Arguments validated; executing tool.", Turn: ex.Turn,
	})
	value, err := s.dispatch(ctx, ex, name, prepared.Args)
	if err != nil {
		outcome := failedTool(err)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			outcome.Status = "cancelled"
		}
		return outcome
	}
	result := s.cfg.redact(toolJSON(value))
	status := "completed"
	if toolResultFailed(result) {
		status = "failed"
	}
	if len(result) > 18000 {
		id := newID()
		if s.store.put("artifacts", id, Memory{ID: id, Principal: ex.Principal, Content: result}) == nil {
			result = fmt.Sprintf("Result stored as artifact %s. Preview: %s", id, clip(result, 3000))
		}
	}
	return toolOutcome{Content: clip(result, 18000), Status: status}
}

func toolResultFailed(content string) bool {
	var receipt struct {
		Error   json.RawMessage `json:"error"`
		IsError bool            `json:"isError"`
	}
	if json.Unmarshal([]byte(content), &receipt) != nil {
		return false
	}
	hasError := len(receipt.Error) > 0 && string(receipt.Error) != "null"
	return hasError || receipt.IsError
}

// A sequential tool makes its whole batch sequential. Read-only batches execute
// concurrently, but model-facing tool receipts always retain assistant order.
func (s *Service) executeBatch(ctx context.Context, ex *Execution, calls []ToolCall) []Message {
	results := make([]Message, len(calls))
	parallel := len(calls) > 1 && s.cfg.ToolConcurrency > 1
	for _, call := range calls {
		tool := s.tools[call.Function.Name]
		if !tool.Parallel || tool.Approval {
			parallel = false
		}
	}
	if !parallel {
		for i, call := range calls {
			content := s.execute(
				ctx,
				ex,
				call,
				false,
			)
			results[i] = Message{Role: "tool", ToolCallID: call.ID, Content: content}
		}
		return results
	}
	prepared := make([]preparedTool, len(calls))
	for i, call := range calls {
		prepared[i] = s.prepareTool(ctx, ex, call)
	}
	var workers sync.WaitGroup
	for i, call := range prepared {
		workers.Add(1)
		go func() {
			defer workers.Done()
			content := s.runPreparedTool(
				ctx,
				ex,
				call,
				false,
			)
			results[i] = Message{Role: "tool", ToolCallID: call.Call.ID, Content: content}
		}()
	}
	workers.Wait()
	return results
}
