package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func (s *Service) recordEvent(runID string, event Event) error {
	if runID == "" {
		return nil
	}
	event.At = time.Now().UTC()
	event.Text = s.cfg.redact(event.Text)
	event.Arguments = clip(s.cfg.redact(event.Arguments), 4000)
	event.Result = clip(s.cfg.redact(event.Result), 6000)
	err := updateRecord(s.store, "runs", runID, func(run *Run) error {
		run.Events = append(run.Events, event)
		run.UpdatedAt = event.At
		updateRunState(run, event)
		return nil
	})
	if err == nil {
		s.notifyRun(runID)
	}
	return err
}

func updateRunState(run *Run, event Event) {
	if run.PendingTools == nil {
		run.PendingTools = []string{}
	}
	switch event.Type {
	case "tool_start":
		run.PendingTools = append(run.PendingTools, event.CallID)
		run.Phase = "tools"
	case "tool_end":
		run.PendingTools = slices.DeleteFunc(run.PendingTools, func(id string) bool { return id == event.CallID })
		if len(run.PendingTools) == 0 {
			run.Phase = "thinking"
		}
	case "turn_start":
		if event.Agent == "lead" {
			run.Turns = event.Turn
			run.Phase = "thinking"
		}
	case "assistant_delta":
		run.Phase = "responding"
	case "agent_start":
		run.Phase = "thinking"
	case "GoalCheckStart":
		run.Phase = "verifying"
	}
}

func (s *Service) notifyRun(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for signal := range s.watchers[id] {
		select {
		case signal <- struct{}{}:
		default:
		}
	}
}

func (s *Service) watchRun(id string) (chan struct{}, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.watchers[id] == nil {
		s.watchers[id] = map[chan struct{}]struct{}{}
	}
	signal := make(chan struct{}, 1)
	s.watchers[id][signal] = struct{}{}
	return signal, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.watchers[id], signal)
		if len(s.watchers[id]) == 0 {
			delete(s.watchers, id)
		}
	}
}

// runEvents replays persisted events before waiting for new ones. Subscribe before
// reading so an update racing the initial snapshot cannot be missed.
func (s *Service) runEvents(c *gin.Context) {
	id := c.Param("id")
	var run Run
	if err := s.store.get("runs", id, &run); err != nil || run.Principal != c.GetString("agent_principal") {
		agentError(c, http.StatusNotFound, ErrNotFound)
		return
	}
	cursor := 0
	raw := c.Query("after")
	if raw == "" {
		raw = c.GetHeader("Last-Event-ID")
	}
	if raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 || parsed > len(run.Events) {
			agentError(c, http.StatusBadRequest, errors.New("invalid event cursor"))
			return
		}
		cursor = parsed
	}
	signal, unwatch := s.watchRun(id)
	defer unwatch()
	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache, no-store, no-transform")
	c.Header("X-Accel-Buffering", "no")
	controller := http.NewResponseController(c.Writer)
	write := func(kind, eventID string, data any) error {
		// Override short server-wide write timeouts only for this stream.
		_ = controller.SetWriteDeadline(time.Now().Add(30 * time.Second))
		encoded, err := json.Marshal(data)
		if err != nil {
			return err
		}
		if eventID != "" {
			if _, err := fmt.Fprintf(c.Writer, "id: %s\n", eventID); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(
			c.Writer,
			"event: %s\ndata: %s\n\n",
			kind,
			encoded,
		); err != nil {
			return err
		}
		return controller.Flush()
	}
	if err := write("run", "", gin.H{"id": id, "status": run.Status, "created_at": run.CreatedAt}); err != nil {
		return
	}
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		if err := s.store.get("runs", id, &run); err != nil {
			return
		}
		for cursor < len(run.Events) {
			event := run.Events[cursor]
			cursor++
			if err := write("activity", strconv.Itoa(cursor), event); err != nil {
				return
			}
		}
		if run.Status != "running" {
			run.Principal = ""
			run.Events = []Event{}
			_ = write("done", "", run)
			return
		}
		select {
		case <-c.Request.Context().Done():
			return
		case <-s.ctx.Done():
			return
		case <-signal:
		case <-heartbeat.C:
			_ = controller.SetWriteDeadline(time.Now().Add(30 * time.Second))
			if _, err := fmt.Fprint(c.Writer, ": keepalive\n\n"); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
		}
	}
}

func (s *Service) completeTurn(ctx context.Context, ex *Execution, input []Message) (Message, error) {
	messageID := newID()
	_ = s.recordEvent(ex.RunID, Event{Type: "model_start", Agent: ex.Agent, MessageID: messageID, Turn: ex.Turn})
	started := time.Now()
	var result Completion
	var err error
	model, streaming := s.model.(StreamingModel)
	if streaming && ex.Depth == 0 {
		if err = ctx.Err(); err == nil {
			err = ex.Budget.reserve()
		}
		if err == nil {
			var pending strings.Builder
			lastFlush := time.Now().Add(-time.Second)
			flush := func() {
				if pending.Len() == 0 {
					return
				}
				_ = s.recordEvent(ex.RunID, Event{
					Type: "assistant_delta", MessageID: messageID, Agent: ex.Agent, Text: pending.String(), Turn: ex.Turn,
				})
				pending.Reset()
				lastFlush = time.Now()
			}
			text := textStream{cfg: s.cfg, emit: func(part string) {
				pending.WriteString(part)
				if time.Since(lastFlush) >= 80*time.Millisecond {
					flush()
				}
			}}
			result, err = model.StreamComplete(
				ctx,
				input,
				s.definitions(ex),
				text.push,
			)
			text.finish()
			flush()
			ex.Budget.add(result.Tokens)
		}
	} else {
		result.Message, err = s.complete(ctx, ex, input, s.definitions(ex))
		if err == nil && ex.Depth == 0 && result.Message.Content != "" {
			_ = s.recordEvent(ex.RunID, Event{
				Type: "assistant_delta", MessageID: messageID, Agent: ex.Agent, Text: result.Message.Content, Turn: ex.Turn,
			})
		}
	}
	status := "completed"
	if err != nil {
		status = "failed"
	}
	_ = s.recordEvent(ex.RunID, Event{
		Type: "model_end", MessageID: messageID, Agent: ex.Agent, Status: status,
		DurationMS: time.Since(started).Milliseconds(), Turn: ex.Turn,
	})
	result.Message.Content = s.cfg.redact(result.Message.Content)
	return result.Message, err
}

// textStream holds possible secret prefixes across provider chunk boundaries.
// Applying redact to each token alone would leak credentials split into tokens.
type textStream struct {
	cfg     Config
	pending string
	emit    func(string)
}

func (s *textStream) push(part string) {
	s.pending += part
	cut := len(s.pending)
	lower := strings.ToLower(s.pending)
	if start := strings.LastIndex(lower, "sk-"); start >= 0 {
		tail := lower[start+3:]
		if strings.Trim(tail, "abcdefghijklmnopqrstuvwxyz0123456789_-") == "" {
			cut = start
		}
	}
	for _, key := range []string{s.cfg.APIKey, s.cfg.OwnerToken, "s", "sk"} {
		if key == "" {
			continue
		}
		for length := min(len(key), len(s.pending)); length > 0; length-- {
			suffix := s.pending[len(s.pending)-length:]
			matches := suffix == key[:length]
			if key == "s" || key == "sk" {
				matches = strings.EqualFold(suffix, key[:length])
			}
			if matches {
				cut = min(cut, len(s.pending)-length)
				break
			}
		}
	}
	if cut > 0 {
		s.emit(s.cfg.redact(s.pending[:cut]))
		s.pending = s.pending[cut:]
	}
}

func (s *textStream) finish() {
	if s.pending != "" {
		s.emit(s.cfg.redact(s.pending))
		s.pending = ""
	}
}
