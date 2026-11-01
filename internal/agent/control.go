package agent

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

var errRunSettling = errors.New("run is finishing; send a new session message")

type QueueInput struct {
	ID      string `json:"id" binding:"max=64"`
	Message string `json:"message" binding:"required,max=8000"`
	Mode    string `json:"mode" binding:"required,oneof=steer follow_up"`
}

func (s *Service) Queue(principal, runID string, input QueueInput) (RunInput, error) {
	message := strings.TrimSpace(input.Message)
	validMode := input.Mode == "steer" || input.Mode == "follow_up"
	if !validMode || message == "" || utf8.RuneCountInString(message) > 8000 || len(input.ID) > 64 {
		return RunInput{}, errors.New("invalid queued message")
	}
	item := RunInput{ID: input.ID, Content: s.cfg.redact(message), Mode: input.Mode, Status: "pending"}
	if item.ID == "" {
		item.ID = newID()
	}
	err := updateRecord(s.store, "runs", runID, func(run *Run) error {
		if run.Principal != principal {
			return ErrNotFound
		}
		for _, existing := range run.Inputs {
			if existing.ID != item.ID {
				continue
			}
			if existing.Mode != item.Mode || existing.Content != item.Content {
				return errors.New("queued message id already has different content")
			}
			item = existing
			return nil
		}
		if run.Status != "running" || run.Phase == "finishing" {
			return errRunSettling
		}
		if len(run.Inputs) >= 32 {
			return errors.New("at most 32 queued messages per run")
		}
		run.Inputs = append(run.Inputs, item)
		run.UpdatedAt = time.Now().UTC()
		run.Events = append(run.Events, Event{
			Type: "input_queued", InputID: item.ID, Mode: item.Mode,
			Text: item.Content, Status: item.Status, At: run.UpdatedAt,
		})
		return nil
	})
	if err == nil {
		s.notifyRun(runID)
	}
	return item, err
}

func (s *Service) RemoveInput(principal, runID, inputID string) error {
	err := updateRecord(s.store, "runs", runID, func(run *Run) error {
		if run.Principal != principal {
			return ErrNotFound
		}
		for i := range run.Inputs {
			item := &run.Inputs[i]
			if item.ID != inputID {
				continue
			}
			if item.Status == "removed" {
				return nil
			}
			if item.Status != "pending" || run.Status != "running" {
				return errors.New("queued message is no longer removable")
			}
			item.Status = "removed"
			run.UpdatedAt = time.Now().UTC()
			run.Events = append(run.Events, Event{
				Type: "input_removed", InputID: item.ID, Mode: item.Mode,
				Text: item.Content, Status: item.Status, At: run.UpdatedAt,
			})
			return nil
		}
		return ErrNotFound
	})
	if err == nil {
		s.notifyRun(runID)
	}
	return err
}

// Picking the last queued input and sealing a finished run share one transaction.
// A message accepted while the model was replying cannot disappear in the gap
// between the loop deciding to stop and work persisting its final status.
func (s *Service) nextInput(ex *Execution, idle bool) (*RunInput, error) {
	if ex.Depth > 0 || ex.RunID == "" {
		return nil, nil
	}
	var selected *RunInput
	err := updateRecord(s.store, "runs", ex.RunID, func(run *Run) error {
		index := -1
		for _, mode := range []string{"steer", "follow_up"} {
			if mode == "follow_up" && !idle {
				continue
			}
			for i, item := range run.Inputs {
				if item.Status == "pending" && item.Mode == mode {
					index = i
					break
				}
			}
			if index >= 0 {
				break
			}
		}
		if index < 0 {
			if idle {
				run.Phase = "finishing"
			}
			return nil
		}
		run.Inputs[index].Status = "delivered"
		item := run.Inputs[index]
		selected = &item
		run.UpdatedAt = time.Now().UTC()
		run.Events = append(run.Events, Event{
			Type: "input_delivered", InputID: item.ID, Mode: item.Mode,
			Text: item.Content, Status: item.Status, At: run.UpdatedAt,
		})
		return nil
	})
	if err == nil && selected != nil {
		s.notifyRun(ex.RunID)
	}
	return selected, err
}

func (s *Service) queueInput(c *gin.Context) {
	var input QueueInput
	if err := c.ShouldBindJSON(&input); err != nil {
		agentError(c, http.StatusBadRequest, errors.New("invalid queued message"))
		return
	}
	var run Run
	principal := c.GetString("agent_principal")
	if err := s.store.get("runs", c.Param("id"), &run); err != nil || run.Principal != principal {
		agentError(c, http.StatusNotFound, ErrNotFound)
		return
	}
	if err := s.quota(c, "chat", 30, 1000); err != nil {
		agentError(c, http.StatusTooManyRequests, err)
		return
	}
	item, err := s.Queue(principal, run.ID, input)
	if err != nil {
		agentError(c, http.StatusConflict, err)
		return
	}
	c.JSON(http.StatusAccepted, item)
}

func (s *Service) removeInput(c *gin.Context) {
	err := s.RemoveInput(c.GetString("agent_principal"), c.Param("id"), c.Param("input"))
	if err != nil {
		status := http.StatusConflict
		if errors.Is(err, ErrNotFound) {
			status = http.StatusNotFound
		}
		agentError(c, status, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "removed"})
}
