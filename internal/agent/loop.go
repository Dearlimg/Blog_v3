package agent

import (
	"context"
	"errors"
)

func (s *Service) loop(
	ctx context.Context,
	ex *Execution,
	messages []Message,
	goal string,
) (string, []Message, error) {
	goalAttempts := 0
	inputSelected := false
	for step := range s.cfg.MaxSteps {
		if err := ctx.Err(); err != nil {
			return "", messages, err
		}
		if !inputSelected {
			item, err := s.nextInput(ex, false)
			if err != nil {
				return "", messages, err
			}
			if item != nil {
				messages = append(messages, Message{Role: "user", Content: item.Content})
			}
		}
		inputSelected = false
		ex.Turn = step + 1
		messages = s.compact(ctx, ex, messages)
		s.injectMail(ex, &messages)
		message, history, err := s.runTurn(
			ctx,
			ex,
			messages,
			goal,
		)
		messages = history
		if err != nil {
			return "", messages, err
		}
		if len(message.ToolCalls) > 0 {
			continue
		}
		if err := s.hook(ctx, HookEvent{Phase: "Stop", RunID: ex.RunID, Agent: ex.Agent}); err != nil {
			return "", messages, err
		}
		if goal != "" {
			goalAttempts++
			passed, reason, err := s.judge(ctx, ex, goal, messages)
			if err != nil {
				return message.Content, messages, err
			}
			if !passed {
				if goalAttempts >= 3 {
					return message.Content, messages, errGoalBlocked
				}
				messages = append(messages, Message{
					Role: "user", Content: "Goal verification has not passed: " + reason +
						". Continue using evidence, or explicitly report a blocker.",
				})
				continue
			}
		}
		item, err := s.nextInput(ex, true)
		if err != nil {
			return message.Content, messages, err
		}
		if item == nil {
			return message.Content, messages, nil
		}
		messages = append(messages, Message{Role: "user", Content: item.Content})
		inputSelected = true
		goalAttempts = 0
	}
	return "", messages, ErrBudget
}

// A turn finalizes the assistant request and all of its tool receipts before
// steering can enter the next request. UI events never become model messages.
func (s *Service) runTurn(
	ctx context.Context,
	ex *Execution,
	messages []Message,
	goal string,
) (Message, []Message, error) {
	_ = s.recordEvent(ex.RunID, Event{Type: "turn_start", Agent: ex.Agent, Turn: ex.Turn})
	message, err := s.completeTurn(ctx, ex, s.prepareRequest(ex, messages, goal))
	if err == nil {
		err = normalizeToolCalls(&message)
	}
	if err == nil {
		messages = append(messages, message)
		messages = append(messages, s.executeBatch(ctx, ex, message.ToolCalls)...)
		err = ctx.Err()
	}
	status := "completed"
	if err != nil {
		status = "failed"
		if errors.Is(err, context.Canceled) {
			status = "cancelled"
		}
	}
	_ = s.recordEvent(ex.RunID, Event{
		Type: "turn_end", Agent: ex.Agent, Turn: ex.Turn, Status: status,
	})
	return message, messages, err
}

func (s *Service) prepareRequest(ex *Execution, messages []Message, goal string) []Message {
	query := ""
	if len(messages) > 0 {
		query = messages[len(messages)-1].Content
	}
	system := systemPrompt(s.recall(ex.Principal, query))
	if goal != "" {
		system += "\nAcceptance criteria for this run: " + goal
	}
	input := make([]Message, 0, len(messages)+1)
	input = append(input, Message{Role: "system", Content: system})
	for _, message := range messages {
		// Receipts marked Internal still belong in model context. Only their
		// storage/UI flag is removed at the provider boundary.
		message.ToolCalls = append([]ToolCall{}, message.ToolCalls...)
		input = append(input, message)
	}
	return input
}

func normalizeToolCalls(message *Message) error {
	if len(message.ToolCalls) > 8 {
		return errors.New("too many tools in one step")
	}
	ids := map[string]bool{}
	for i := range message.ToolCalls {
		call := &message.ToolCalls[i]
		if call.ID == "" {
			call.ID = newID()
		}
		if ids[call.ID] {
			return errors.New("duplicate tool call id in one step")
		}
		ids[call.ID] = true
	}
	return nil
}
