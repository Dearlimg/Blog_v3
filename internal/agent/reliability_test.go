package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type barrierModel struct {
	calls atomic.Int32
	count int32
	ready chan struct{}
}

func (m *barrierModel) Complete(
	ctx context.Context,
	_ []Message,
	_ []ToolDefinition,
) (Completion, error) {
	call := m.calls.Add(1)
	if call == m.count {
		close(m.ready)
	}
	if call <= m.count {
		select {
		case <-ctx.Done():
			return Completion{}, ctx.Err()
		case <-m.ready:
		}
	}
	return Completion{Message: Message{Role: "assistant", Content: "done"}, Tokens: 1}, nil
}

func reliabilityConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		APIKey: "test-key", DataPath: filepath.Join(t.TempDir(), "agent.db"),
		Concurrency: 3, MaxCalls: 24, MaxTokens: 80000, MaxSteps: 12,
		RunTimeout: time.Second * 10,
	}
}

func reliabilityService(t *testing.T, model Model) *Service {
	t.Helper()
	s, err := NewService(reliabilityConfig(t), model)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestQuotaTrustsOnlyConfiguredProxies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name    string
		proxies []string
		want    []int
	}{
		{"trusted", []string{"172.18.0.2"}, []int{200, 200, 429}},
		{"untrusted", []string{}, []int{200, 429, 429}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := reliabilityService(t, nil)
			r := gin.New()
			if err := r.SetTrustedProxies(test.proxies); err != nil {
				t.Fatal(err)
			}
			r.GET("/quota", func(c *gin.Context) {
				if err := s.quota(c, "test", 1, 100); err != nil {
					c.Status(http.StatusTooManyRequests)
					return
				}
				c.Status(http.StatusOK)
			})
			for index, client := range []string{"8.8.8.8", "1.1.1.1", "8.8.8.8"} {
				req := httptest.NewRequest(http.MethodGet, "/quota", nil)
				req.RemoteAddr = "172.18.0.2:40000"
				// A client-controlled prefix must not replace the rightmost untrusted hop.
				req.Header.Set("X-Forwarded-For", "9.9.9.9, "+client)
				recorder := httptest.NewRecorder()
				r.ServeHTTP(recorder, req)
				if recorder.Code != test.want[index] {
					t.Fatalf("request %d: got %d, want %d", index, recorder.Code, test.want[index])
				}
			}
		})
	}
}

func TestParallelTeamPreservesMemberContexts(t *testing.T) {
	model := &barrierModel{count: 3, ready: make(chan struct{})}
	s := reliabilityService(t, model)
	team := Team{
		ID: newID(), Principal: "owner", Status: "running", Plan: "Research",
		Members: []string{"one", "two", "three"}, Contexts: map[string][]Message{},
	}
	for _, member := range team.Members {
		team.Contexts[member] = []Message{{Role: "user", Content: member}}
	}
	ex := &Execution{
		Principal: "owner", Agent: "lead", Owner: true,
		Budget: &Budget{maxCalls: 24, maxTokens: 80000},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	if _, err := s.runTeam(ctx, ex, team, "Investigate"); err != nil {
		t.Fatal(err)
	}
	var saved Team
	if err := s.store.get("teams", team.ID, &saved); err != nil {
		t.Fatal(err)
	}
	for _, member := range team.Members {
		history := saved.Contexts[member]
		if len(history) != 3 || history[0].Content != member || history[2].Content != "done" {
			t.Fatalf("member %s lost its context: %+v", member, history)
		}
	}
	if saved.Status != "idle" || model.calls.Load() != 3 {
		t.Fatalf("unexpected team result: %+v", saved)
	}
}

func TestParallelWorkflowAndCheckpointResume(t *testing.T) {
	for _, resumed := range []bool{false, true} {
		t.Run(map[bool]string{false: "parallel", true: "resume"}[resumed], func(t *testing.T) {
			count := int32(2)
			if resumed {
				count = 1
			}
			model := &barrierModel{count: count, ready: make(chan struct{})}
			s := reliabilityService(t, model)
			args := Args{Name: "research-brief", Prompt: "Investigate"}
			if resumed {
				wf := Workflow{
					ID: newID(), Principal: "owner", Name: args.Name, Prompt: args.Prompt,
					Status: "interrupted", Steps: map[string]string{"primary-sources": "checkpoint"},
				}
				if err := s.store.put("workflows", wf.ID, wf); err != nil {
					t.Fatal(err)
				}
				args.ID = wf.ID
			}
			ex := &Execution{
				Principal: "owner", Agent: "lead", Owner: true,
				Budget: &Budget{maxCalls: 24, maxTokens: 80000},
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
			defer cancel()
			result, err := s.workflow(ctx, ex, args)
			if err != nil {
				t.Fatal(err)
			}
			wf := result.(Workflow)
			if wf.Status != "completed" || len(wf.Steps) != 3 || model.calls.Load() != count+1 {
				t.Fatalf("unexpected workflow: %+v, calls=%d", wf, model.calls.Load())
			}
			if resumed && wf.Steps["primary-sources"] != "checkpoint" {
				t.Fatal("completed checkpoint was rerun")
			}
		})
	}
}

func TestRestartRecoversStateWithoutReplayingApprovals(t *testing.T) {
	cfg := reliabilityConfig(t)
	store, err := OpenStore(cfg.DataPath)
	if err != nil {
		t.Fatal(err)
	}
	session := Session{ID: newID(), Principal: "owner", ActiveRun: "old-run", Messages: []Message{}}
	team := Team{
		ID: newID(), Principal: "owner", Status: "running", Members: []string{"one"},
		Contexts: map[string][]Message{"one": {{Role: "user", Content: "checkpoint"}}},
	}
	wf := Workflow{
		ID: newID(), Principal: "owner", Status: "running",
		Steps: map[string]string{"primary-sources": "checkpoint"},
	}
	approval := Approval{
		ID: newID(), Principal: "owner", SessionID: session.ID, Tool: "mcp_call",
		Status: "executing", Arguments: json.RawMessage(`{}`), CreatedAt: time.Now().Add(-2 * time.Hour),
	}
	for bucket, item := range map[string]any{
		"sessions": session, "teams": team, "workflows": wf, "approvals": approval,
		"runs": Run{ID: "old-run", Status: "running"},
	} {
		id := map[string]string{
			"sessions": session.ID, "teams": team.ID, "workflows": wf.ID,
			"approvals": approval.ID, "runs": "old-run",
		}[bucket]
		if err := store.put(bucket, id, item); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := NewService(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	var run Run
	for bucket, target := range map[string]any{
		"sessions": &session, "teams": &team, "workflows": &wf, "approvals": &approval, "runs": &run,
	} {
		id := map[string]string{
			"sessions": session.ID, "teams": team.ID, "workflows": wf.ID,
			"approvals": approval.ID, "runs": "old-run",
		}[bucket]
		if err := s.store.get(bucket, id, target); err != nil {
			t.Fatal(err)
		}
	}
	if run.Status != "interrupted" || session.ActiveRun != "" || team.Status != "idle" {
		t.Fatalf("in-flight state not recovered: run=%s session=%s team=%s", run.Status, session.ActiveRun, team.Status)
	}
	if wf.Status != "interrupted" || wf.Steps["primary-sources"] != "checkpoint" {
		t.Fatalf("workflow checkpoint lost: %+v", wf)
	}
	if team.Contexts["one"][0].Content != "checkpoint" {
		t.Fatal("team checkpoint lost")
	}
	if approval.Status != "interrupted" || !strings.Contains(approval.Result, "Outcome unknown") {
		t.Fatalf("approval uncertainty not preserved: %+v", approval)
	}
	ex := &Execution{Principal: "owner", SessionID: session.ID, Owner: true}
	if _, err := s.approve(ex, approval.ID, true); err == nil {
		t.Fatal("interrupted external action must not be replayed")
	}
	// Recovery is idempotent, and interrupted receipts remain visible even after expiry.
	if err := s.recover(); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/sessions/:id", func(c *gin.Context) {
		c.Set("agent_principal", "owner")
		s.sessionState(c)
	})
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID, nil))
	var state struct {
		Approvals []Approval `json:"approvals"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != 200 || len(state.Approvals) != 1 || state.Approvals[0].Status != "interrupted" {
		t.Fatalf("interrupted approval hidden from user: %s", recorder.Body.String())
	}
	if _, err := s.teamTool(context.Background(), ex, "team_shutdown", Args{ID: team.ID}); err != nil {
		t.Fatalf("recovered team remains stuck: %v", err)
	}
}
