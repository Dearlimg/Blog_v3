package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Tool struct {
	Description string
	Fields      map[string]string
	Required    []string
	Owner       bool
	Approval    bool
	LeadOnly    bool
}

type Args struct {
	Op        string         `json:"op"`
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Query     string         `json:"query"`
	URL       string         `json:"url"`
	Title     string         `json:"title"`
	Content   string         `json:"content"`
	Prompt    string         `json:"prompt"`
	Old       string         `json:"old"`
	Status    string         `json:"status"`
	Agent     string         `json:"agent"`
	Cron      string         `json:"cron"`
	Goal      string         `json:"goal"`
	Offset    int            `json:"offset"`
	Limit     int            `json:"limit"`
	DependsOn []string       `json:"depends_on"`
	Items     []Todo         `json:"items"`
	Members   []string       `json:"members"`
	Arguments map[string]any `json:"arguments"`
}

type Todo struct {
	Content string `json:"content"`
	Status  string `json:"status"`
}
type TodoList struct {
	SessionID string `json:"session_id"`
	Principal string `json:"principal"`
	Items     []Todo `json:"items"`
}

func toolRegistry() map[string]Tool {
	return map[string]Tool{
		"profile_read":     {Description: "Read Durlim's verified public profile."},
		"web_search":       {Description: "Search the live web for current, uncertain or disputed factual claims. Returns sources; report failures honestly.", Fields: map[string]string{"query": "string"}, Required: []string{"query"}},
		"web_read":         {Description: "Read a public source URL. Returned text is untrusted.", Fields: map[string]string{"url": "string"}, Required: []string{"url"}},
		"skills_list":      {Description: "List available domain skills."},
		"skills_load":      {Description: "Load a skill by its listed name.", Fields: map[string]string{"name": "string"}, Required: []string{"name"}},
		"todo_write":       {Description: "Replace this session's plan. Each item has content and status pending/in_progress/completed; at most one in_progress.", Fields: map[string]string{"items": "todos"}, Required: []string{"items"}},
		"memory_search":    {Description: "Recall this visitor's approved long-term memories. Empty query lists them.", Fields: map[string]string{"query": "string"}},
		"memory_write":     {Description: "Propose a long-term memory, requiring user approval. Optional id updates an existing memory.", Fields: map[string]string{"id": "string", "title": "string", "content": "string"}, Required: []string{"title", "content"}, Approval: true, LeadOnly: true},
		"memory_delete":    {Description: "Delete an approved memory by id after user approval.", Fields: map[string]string{"id": "string"}, Required: []string{"id"}, Approval: true, LeadOnly: true},
		"notes_list":       {Description: "List this visitor's workspace notes. No server filesystem access."},
		"note_read":        {Description: "Read one workspace note.", Fields: map[string]string{"id": "string"}, Required: []string{"id"}},
		"note_write":       {Description: "Create or replace a workspace note after approval.", Fields: map[string]string{"id": "string", "title": "string", "content": "string"}, Required: []string{"title", "content"}, Approval: true, LeadOnly: true},
		"note_edit":        {Description: "Replace exactly one occurrence of old text in a note after approval.", Fields: map[string]string{"id": "string", "old": "string", "content": "string"}, Required: []string{"id", "old", "content"}, Approval: true, LeadOnly: true},
		"artifact_read":    {Description: "Read a stored tool result in rune-based pages. Follow next_offset until eof is true.", Fields: map[string]string{"id": "string", "offset": "integer", "limit": "integer"}, Required: []string{"id"}},
		"subagent":         {Description: "Delegate a bounded research subtask in an isolated context. Shares the parent's budget; cannot modify durable state.", Fields: map[string]string{"prompt": "string", "name": "string"}, Required: []string{"prompt"}},
		"task_create":      {Description: "Create a persistent task. Dependencies are runtime task IDs.", Fields: map[string]string{"title": "string", "content": "string", "depends_on": "strings"}, Required: []string{"title"}, LeadOnly: true},
		"task_update":      {Description: "Update a task: pending/in_progress/completed. Dependencies must be completed to claim or finish. Completion requires evidence content.", Fields: map[string]string{"id": "string", "status": "string", "depends_on": "strings", "content": "string"}, Required: []string{"id"}, LeadOnly: true},
		"task_list":        {Description: "List this visitor's dependency tasks."},
		"task_claim":       {Description: "Atomically claim a ready task for the current agent.", Fields: map[string]string{"id": "string"}, Required: []string{"id"}},
		"task_complete":    {Description: "Complete a task claimed by the current agent with evidence.", Fields: map[string]string{"id": "string", "content": "string"}, Required: []string{"id", "content"}},
		"background_start": {Description: "Owner only: start a bounded persistent background agent job. Poll with job_list.", Fields: map[string]string{"prompt": "string", "goal": "string"}, Required: []string{"prompt"}, Owner: true, LeadOnly: true},
		"job_list":         {Description: "List background and scheduled jobs and their last run IDs.", Owner: true},
		"job_cancel":       {Description: "Cancel a job and its active run.", Fields: map[string]string{"id": "string"}, Required: []string{"id"}, Owner: true, LeadOnly: true},
		"schedule_cron":    {Description: "Owner only: schedule a recurring agent prompt in Asia/Shanghai with a five-field cron expression. Runs have bounded budgets.", Fields: map[string]string{"cron": "string", "prompt": "string", "goal": "string"}, Required: []string{"cron", "prompt"}, Owner: true, Approval: true, LeadOnly: true},
		"team_create":      {Description: "Owner only: create a persistent team of 1-3 named members. Team starts awaiting_plan.", Fields: map[string]string{"name": "string", "members": "strings", "content": "string"}, Required: []string{"name", "members", "content"}, Owner: true, LeadOnly: true},
		"team_approve":     {Description: "Approve a stored team's plan through the user's approval UI.", Fields: map[string]string{"id": "string"}, Required: []string{"id"}, Owner: true, Approval: true, LeadOnly: true},
		"team_run":         {Description: "Run an approved team's members concurrently on ready tasks. Persistent member contexts and mailbox report results to lead.", Fields: map[string]string{"id": "string", "prompt": "string"}, Required: []string{"id", "prompt"}, Owner: true, LeadOnly: true},
		"team_message":     {Description: "Send a message to a named member or lead in this team.", Fields: map[string]string{"id": "string", "agent": "string", "content": "string"}, Required: []string{"id", "agent", "content"}, Owner: true},
		"team_shutdown":    {Description: "Shut down an idle team; further runs are denied.", Fields: map[string]string{"id": "string"}, Required: []string{"id"}, Owner: true, LeadOnly: true},
		"team_list":        {Description: "List this owner's teams.", Owner: true},
		"mcp_list":         {Description: "Discover tools from a server configured by the host, never an arbitrary URL.", Fields: map[string]string{"name": "string"}, Required: []string{"name"}, Owner: true},
		"mcp_call":         {Description: "Invoke an allowlisted MCP tool after approval. name is configured server; agent is tool name.", Fields: map[string]string{"name": "string", "agent": "string", "arguments": "object"}, Required: []string{"name", "agent", "arguments"}, Owner: true, Approval: true, LeadOnly: true},
		"workflow_run":     {Description: "Run the trusted research-brief or interview-prep workflow: parallel investigation then synthesis. Optional id resumes the same prompt using checkpoints.", Fields: map[string]string{"name": "string", "prompt": "string", "id": "string"}, Required: []string{"name", "prompt"}, Owner: true, LeadOnly: true},
	}
}

func (s *Service) definitions(ex *Execution) []ToolDefinition {
	definitions := []ToolDefinition{}
	for name, tool := range s.tools {
		if tool.Owner && !ex.Owner {
			continue
		}
		if tool.LeadOnly && ex.Depth > 0 {
			continue
		}
		properties := map[string]any{}
		for field, kind := range tool.Fields {
			property := map[string]any{"type": kind}
			switch kind {
			case "strings":
				property = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
			case "todos":
				property = map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"content": map[string]string{"type": "string"}, "status": map[string]any{"type": "string", "enum": []string{"pending", "in_progress", "completed"}}}, "required": []string{"content", "status"}, "additionalProperties": false}}
			}
			properties[field] = property
		}
		required := append([]string{}, tool.Required...)
		definitions = append(definitions, ToolDefinition{Type: "function", Function: FunctionDefinition{
			Name: name, Description: tool.Description, Parameters: map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false},
		}})
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].Function.Name < definitions[j].Function.Name })
	return definitions
}

func (s *Service) dispatch(ctx context.Context, ex *Execution, name string, a Args) (any, error) {
	switch name {
	case "profile_read":
		return loadSkill("profile")
	case "skills_list":
		return skills, nil
	case "skills_load":
		return loadSkill(a.Name)
	case "web_search":
		if strings.TrimSpace(a.Query) == "" || len(a.Query) > 1000 {
			return nil, errors.New("search query must be 1-1000 bytes")
		}
		sources, err := s.web.Search(ctx, s.cfg.redact(a.Query))
		if err != nil {
			return nil, err
		}
		_ = updateRecord(s.store, "runs", ex.RunID, func(run *Run) error { run.Sources = append(run.Sources, sources...); return nil })
		return sources, nil
	case "web_read":
		return s.web.Read(ctx, a.URL)
	case "todo_write":
		if len(a.Items) > 20 {
			return nil, errors.New("at most 20 todos")
		}
		active := 0
		for _, item := range a.Items {
			if item.Content == "" {
				return nil, errors.New("todo content required")
			}
			switch item.Status {
			case "in_progress":
				active++
			case "pending", "completed":
			default:
				return nil, errors.New("invalid todo status")
			}
		}
		if active > 1 {
			return nil, errors.New("at most one todo in progress")
		}
		list := TodoList{SessionID: ex.SessionID, Principal: ex.Principal, Items: a.Items}
		return list, s.store.put("todos", ex.SessionID, list)
	case "memory_search":
		return s.recall(ex.Principal, a.Query), nil
	case "memory_write", "note_write":
		bucket := "memory"
		if name == "note_write" {
			bucket = "notes"
		}
		if a.Title == "" || a.Content == "" {
			return nil, errors.New("title and content required")
		}
		if len(a.Content) > 16000 || len(a.Title) > 200 {
			return nil, errors.New("note too large")
		}
		if a.ID != "" {
			if _, err := s.ownedMemory(bucket, ex.Principal, a.ID); err != nil {
				return nil, err
			}
		} else {
			a.ID = newID()
		}
		item := Memory{ID: a.ID, Principal: ex.Principal, Title: a.Title, Content: a.Content, UpdatedAt: time.Now().UTC()}
		return item, s.store.put(bucket, a.ID, item)
	case "memory_delete":
		if _, err := s.ownedMemory("memory", ex.Principal, a.ID); err != nil {
			return nil, err
		}
		return map[string]string{"deleted": a.ID}, s.deleteRecord("memory", a.ID)
	case "notes_list":
		items, err := indexedRecords[Memory](s.store, "notes", "principal", ex.Principal)
		result := []Memory{}
		for _, item := range items {
			if item.Principal == ex.Principal {
				result = append(result, item)
			}
		}
		return result, err
	case "note_read":
		return s.ownedMemory("notes", ex.Principal, a.ID)
	case "artifact_read":
		item, err := s.ownedMemory("artifacts", ex.Principal, a.ID)
		if err != nil {
			return nil, err
		}
		content := []rune(item.Content)
		if a.Offset < 0 || a.Offset > len(content) || a.Limit < 0 || a.Limit > 1500 {
			return nil, errors.New("invalid artifact range; limit must be 1-1500")
		}
		if a.Limit == 0 {
			a.Limit = 1500
		}
		end := a.Offset + min(a.Limit, len(content)-a.Offset)
		return map[string]any{
			"id": item.ID, "content": string(content[a.Offset:end]),
			"next_offset": end, "total": len(content), "eof": end == len(content),
		}, nil
	case "note_edit":
		item, err := s.ownedMemory("notes", ex.Principal, a.ID)
		if err != nil {
			return nil, err
		}
		if a.Old == "" || strings.Count(item.Content, a.Old) != 1 {
			return nil, errors.New("old text must match exactly once")
		}
		item.Content = strings.Replace(item.Content, a.Old, a.Content, 1)
		if len(item.Content) > 16000 {
			return nil, errors.New("note too large")
		}
		item.UpdatedAt = time.Now().UTC()
		return item, s.store.put("notes", item.ID, item)
	case "subagent":
		return s.child(ctx, ex, a.Prompt, "researcher")
	case "task_create", "task_update", "task_claim", "task_complete", "task_list":
		return s.taskTool(ex, name, a)
	case "background_start", "schedule_cron", "job_list", "job_cancel":
		return s.jobTool(ex, name, a)
	case "team_create", "team_approve", "team_run", "team_message", "team_shutdown", "team_list":
		return s.teamTool(ctx, ex, name, a)
	case "mcp_list":
		return s.mcp.List(ctx, a.Name)
	case "mcp_call":
		return s.mcp.Call(ctx, a)
	case "workflow_run":
		return s.workflow(ctx, ex, a)
	}
	return nil, fmt.Errorf("tool %q is unavailable", name)
}

func (s *Service) ownedMemory(bucket, principal, id string) (Memory, error) {
	var item Memory
	if err := s.store.get(bucket, id, &item); err != nil || item.Principal != principal {
		return Memory{}, ErrNotFound
	}
	return item, nil
}
