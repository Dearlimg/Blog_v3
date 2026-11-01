package agent

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	bolt "go.etcd.io/bbolt"
)

type ChatInput struct {
	Message string `json:"message" binding:"required,max=8000"`
	Goal    string `json:"goal" binding:"max=2000"`
}

type Capability struct {
	Lesson int    `json:"lesson"`
	Name   string `json:"name"`
	Scope  string `json:"scope"`
}

var capabilities = []Capability{
	{Lesson: 1, Name: "Agent loop", Scope: "visitor"},
	{Lesson: 2, Name: "Tool dispatch", Scope: "visitor"},
	{Lesson: 3, Name: "Permission and approval", Scope: "visitor"},
	{Lesson: 4, Name: "Lifecycle hooks", Scope: "host"},
	{Lesson: 5, Name: "Todo plan", Scope: "visitor"},
	{Lesson: 6, Name: "Isolated subagents", Scope: "visitor"},
	{Lesson: 7, Name: "On-demand skills", Scope: "visitor"},
	{Lesson: 8, Name: "Context compaction", Scope: "visitor"},
	{Lesson: 9, Name: "Persistent memory", Scope: "approval"},
	{Lesson: 10, Name: "Dependency tasks", Scope: "visitor"},
	{Lesson: 11, Name: "Background jobs", Scope: "owner"},
	{Lesson: 12, Name: "Cron scheduler", Scope: "owner+approval"},
	{Lesson: 13, Name: "Agent teams", Scope: "owner+approval"},
	{Lesson: 14, Name: "MCP tools", Scope: "owner+configuration+approval"},
	{Lesson: 15, Name: "Integrated harness", Scope: "visitor"},
	{Lesson: 16, Name: "Resumable workflows", Scope: "owner"},
	{Lesson: 17, Name: "Independent goal gate", Scope: "visitor"},
}

func (s *Service) Register(r *gin.Engine) {
	api := r.Group("/api/v1/agent")
	api.Use(s.identity)
	api.GET("/info", func(c *gin.Context) {
		c.JSON(200, gin.H{"configured": s.cfg.APIKey != "", "owner": c.GetBool("agent_owner"), "capabilities": capabilities})
	})
	api.POST("/sessions", s.newSession)
	api.GET("/sessions/:id", s.sessionState)
	api.POST("/sessions/:id/messages", s.chat)
	api.GET("/runs/:id", s.runState)
	api.POST("/runs/:id/cancel", func(c *gin.Context) {
		if err := s.Cancel(c.GetString("agent_principal"), c.Param("id")); err != nil {
			agentError(c, 404, err)
			return
		}
		c.JSON(200, gin.H{"status": "cancellation_requested"})
	})
	api.POST("/sessions/:id/approvals/:approval", s.resolveApproval)
	api.GET("/owner/state", s.ownerState)
	api.POST("/owner/tools/:tool", s.ownerTool)
}

func agentError(c *gin.Context, status int, err error) {
	c.AbortWithStatusJSON(status, gin.H{"error": err.Error()})
}

func validID(id string) bool { data, err := hex.DecodeString(id); return err == nil && len(data) == 24 }

func (s *Service) identity(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	if origin := c.GetHeader("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host != c.Request.Host {
			agentError(c, 403, errors.New("cross-origin request denied"))
			return
		}
	}
	if c.GetHeader("Sec-Fetch-Site") == "cross-site" {
		agentError(c, 403, errors.New("cross-site request denied"))
		return
	}
	principal, _ := c.Cookie("durlim_agent")
	if !validID(principal) {
		principal = newID()
	}
	// Refresh existing cookies too, so HTTPS upgrades legacy non-Secure cookies.
	http.SetCookie(c.Writer, &http.Cookie{Name: "durlim_agent", Value: principal,
		Path: "/api/v1/agent", HttpOnly: true, Secure: s.secureRequest(c),
		SameSite: http.SameSiteStrictMode, MaxAge: 30 * 24 * 60 * 60})
	token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	owner := s.cfg.OwnerToken != "" && subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.OwnerToken)) == 1
	if owner {
		host, _, _ := net.SplitHostPort(c.Request.RemoteAddr)
		local := net.ParseIP(host).IsLoopback() && c.GetHeader("X-Forwarded-For") == "" && c.GetHeader("X-Forwarded-Proto") == ""
		if !s.secureRequest(c) && !local {
			agentError(c, 403, errors.New("owner access requires HTTPS or a local SSH tunnel"))
			return
		}
		principal = "owner"
	}
	c.Set("agent_principal", principal)
	c.Set("agent_owner", owner)
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16000)
	c.Next()
}

func (s *Service) newSession(c *gin.Context) {
	principal := c.GetString("agent_principal")
	if err := s.quota(c, "session", 10, 300); err != nil {
		agentError(c, 429, err)
		return
	}
	session, err := s.createSession(principal)
	if err != nil {
		agentError(c, 500, errors.New("cannot create session"))
		return
	}
	c.JSON(201, gin.H{"id": session.ID})
}

func (s *Service) chat(c *gin.Context) {
	var input ChatInput
	if err := c.ShouldBindJSON(&input); err != nil || strings.TrimSpace(input.Message) == "" {
		agentError(c, 400, errors.New("invalid message"))
		return
	}
	var session Session
	if err := s.store.get("sessions", c.Param("id"), &session); err != nil || session.Principal != c.GetString("agent_principal") {
		agentError(c, 404, ErrNotFound)
		return
	}
	if err := s.quota(c, "chat", 30, 1000); err != nil {
		agentError(c, 429, err)
		return
	}
	run, err := s.Start(session.Principal, session.ID, input, c.GetBool("agent_owner"))
	if err != nil {
		agentError(c, 409, err)
		return
	}
	c.JSON(202, gin.H{"run_id": run.ID})
}

func (s *Service) sessionState(c *gin.Context) {
	var session Session
	if err := s.store.get("sessions", c.Param("id"), &session); err != nil || session.Principal != c.GetString("agent_principal") {
		agentError(c, 404, ErrNotFound)
		return
	}
	messages := []Message{}
	for _, m := range session.Messages {
		if !m.Internal && (m.Role == "user" || m.Role == "assistant") && len(m.ToolCalls) == 0 {
			messages = append(messages, Message{Role: m.Role, Content: m.Content})
		}
	}
	approvals, _ := indexedRecords[Approval](s.store, "approvals", "session_id", session.ID)
	pending := []Approval{}
	for _, approval := range approvals {
		if approval.Principal != session.Principal || approval.SessionID != session.ID {
			continue
		}
		if approval.Status == "interrupted" ||
			(approval.Status == "pending" && time.Since(approval.CreatedAt) <= time.Hour) {
			pending = append(pending, approval)
		}
	}
	var todos TodoList
	_ = s.store.get("todos", session.ID, &todos)
	if todos.Items == nil {
		todos.Items = []Todo{}
	}
	activeRun := session.ActiveRun
	if strings.HasPrefix(activeRun, "approval:") {
		activeRun = ""
	}
	c.JSON(200, gin.H{"id": session.ID, "messages": messages, "active_run": activeRun, "approvals": pending, "todos": todos.Items})
}

func (s *Service) runState(c *gin.Context) {
	var run Run
	if err := s.store.get("runs", c.Param("id"), &run); err != nil || run.Principal != c.GetString("agent_principal") {
		agentError(c, 404, ErrNotFound)
		return
	}
	run.Principal = ""
	c.JSON(200, run)
}

func (s *Service) resolveApproval(c *gin.Context) {
	var input struct {
		Allow *bool `json:"allow"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.Allow == nil {
		agentError(c, 400, errors.New("allow is required"))
		return
	}
	var session Session
	if err := s.store.get("sessions", c.Param("id"), &session); err != nil || session.Principal != c.GetString("agent_principal") {
		agentError(c, 404, ErrNotFound)
		return
	}
	if session.ActiveRun != "" {
		agentError(c, 409, ErrBusy)
		return
	}
	ex := &Execution{Service: s, Principal: session.Principal, SessionID: session.ID, Owner: c.GetBool("agent_owner"), Agent: "lead"}
	approval, err := s.approve(ex, c.Param("approval"), *input.Allow)
	if err != nil {
		agentError(c, 400, err)
		return
	}
	c.JSON(200, approval)
}

func (s *Service) quota(c *gin.Context, kind string, perIP, global int) error {
	if c.GetBool("agent_owner") {
		perIP *= 10
	}
	// ClientIP only honors forwarding headers from the engine's trusted proxies.
	hash := sha256.Sum256([]byte(c.ClientIP()))
	day := time.Now().UTC().Format("2006-01-02")
	keys := []string{day + ":" + kind + ":" + hex.EncodeToString(hash[:]), day + ":" + kind + ":global"}
	return s.store.db.Update(func(tx *bolt.Tx) error {
		for i, key := range keys {
			count := 0
			_ = readRecord(tx, "limits", key, &count)
			limit := perIP
			if i == 1 {
				limit = global
			}
			if count >= limit {
				return errors.New("daily request limit reached")
			}
			if err := writeRecord(tx, "limits", key, count+1); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) ownerState(c *gin.Context) {
	if !c.GetBool("agent_owner") {
		agentError(c, 403, errors.New("owner access required"))
		return
	}
	jobs, _ := indexedRecords[Job](s.store, "jobs", "principal", "owner")
	teams, _ := indexedRecords[Team](s.store, "teams", "principal", "owner")
	workflows, _ := indexedRecords[Workflow](s.store, "workflows", "principal", "owner")
	tasks, _ := indexedRecords[Task](s.store, "tasks", "principal", "owner")
	owned := []Task{}
	for _, task := range tasks {
		if task.Principal == "owner" {
			owned = append(owned, task)
		}
	}
	sort.Slice(owned, func(i, j int) bool { return owned[i].UpdatedAt.After(owned[j].UpdatedAt) })
	c.JSON(200, gin.H{"jobs": jobs, "teams": teams, "workflows": workflows, "tasks": owned})
}

func (s *Service) ownerTool(c *gin.Context) {
	if !c.GetBool("agent_owner") {
		agentError(c, 403, errors.New("owner access required"))
		return
	}
	var input struct {
		SessionID string `json:"session_id"`
		Arguments Args   `json:"arguments"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		agentError(c, 400, errors.New("invalid tool request"))
		return
	}
	var session Session
	if err := s.store.get("sessions", input.SessionID, &session); err != nil || session.Principal != "owner" {
		agentError(c, 404, ErrNotFound)
		return
	}
	if session.ActiveRun != "" {
		agentError(c, 409, ErrBusy)
		return
	}
	name := c.Param("tool")
	// Long-running tools go through the bounded agent run, not an unbounded HTTP handler.
	if name == "team_run" || name == "workflow_run" || name == "subagent" {
		agentError(c, 400, errors.New("use a chat run for this tool"))
		return
	}
	ex := &Execution{Service: s, Principal: "owner", SessionID: session.ID, Owner: true, Agent: "lead"}
	call := ToolCall{ID: newID(), Function: FunctionCall{Name: name, Arguments: toolJSON(input.Arguments)}}
	c.Data(200, "application/json", []byte(s.execute(c.Request.Context(), ex, call, false)))
}
