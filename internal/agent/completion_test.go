package agent

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	bolt "go.etcd.io/bbolt"
)

func TestJobMirrorsRunOutcome(t *testing.T) {
	for _, outcome := range []string{"completed", "failed", "cancelled", "budget_exhausted", "blocked", "interrupted", "missing"} {
		t.Run(outcome, func(t *testing.T) {
			s := reliabilityService(t, nil)
			run := Run{ID: newID(), Principal: "owner", Status: outcome}
			if outcome != "completed" {
				run.Error = "reason: " + outcome
			}
			if outcome != "missing" {
				if err := s.store.put("runs", run.ID, run); err != nil {
					t.Fatal(err)
				}
			}
			job := Job{ID: newID(), Principal: "owner", Status: "active", RunID: run.ID}
			if err := s.store.put("jobs", job.ID, job); err != nil {
				t.Fatal(err)
			}
			s.tickJobs(time.Now())
			if err := s.store.get("jobs", job.ID, &job); err != nil {
				t.Fatal(err)
			}
			want := outcome
			if want == "missing" {
				want = "failed"
			}
			if job.Status != want || (outcome != "completed" && job.LastError == "") {
				t.Fatalf("lost run outcome: %+v", job)
			}
		})
	}
}

func TestArtifactPagesReadEntireUnicodeResult(t *testing.T) {
	s := reliabilityService(t, nil)
	id := newID()
	content := strings.Repeat("中文😀\"<\n", 6000) + "FINAL_TAIL"
	if err := s.store.put("artifacts", id, Memory{ID: id, Principal: "visitor", Content: content}); err != nil {
		t.Fatal(err)
	}
	ex := &Execution{Principal: "visitor", Agent: "lead"}
	var combined strings.Builder
	offset := 0
	for range 100 {
		result := s.execute(context.Background(), ex, ToolCall{Function: FunctionCall{
			Name: "artifact_read", Arguments: toolJSON(map[string]any{"id": id, "offset": offset}),
		}}, false)
		var page struct {
			Content string `json:"content"`
			Next    int    `json:"next_offset"`
			EOF     bool   `json:"eof"`
		}
		if err := json.Unmarshal([]byte(result), &page); err != nil {
			t.Fatalf("page was wrapped or truncated: %.100s", result)
		}
		combined.WriteString(page.Content)
		if page.EOF {
			break
		}
		if page.Next <= offset {
			t.Fatal("page cursor did not advance")
		}
		offset = page.Next
	}
	if combined.String() != content {
		t.Fatal("artifact tail or unicode content lost")
	}
	items, err := records[Memory](s.store, "artifacts")
	if err != nil || len(items) != 1 {
		t.Fatalf("reading created extra artifacts: %v count=%d", err, len(items))
	}
	if _, err := s.dispatch(context.Background(), &Execution{Principal: "other"}, "artifact_read", Args{ID: id}); err == nil {
		t.Fatal("artifact ownership bypassed")
	}
	if _, err := s.dispatch(context.Background(), ex, "artifact_read", Args{ID: id, Offset: -1}); err == nil {
		t.Fatal("negative range accepted")
	}
}

func TestApprovalReceiptIsAtomicAndAvailableToNextTurn(t *testing.T) {
	for _, allow := range []bool{true, false} {
		t.Run(map[bool]string{true: "allow", false: "deny"}[allow], func(t *testing.T) {
			s := reliabilityService(t, nil)
			session, err := s.createSession("visitor")
			if err != nil {
				t.Fatal(err)
			}
			approval := Approval{
				ID: newID(), SessionID: session.ID, Principal: "visitor", Tool: "note_write",
				Arguments: json.RawMessage(`{"title":"Note","content":"Evidence"}`),
				Status:    "pending", CreatedAt: time.Now(),
			}
			if err := s.store.put("approvals", approval.ID, approval); err != nil {
				t.Fatal(err)
			}
			ex := &Execution{Principal: "visitor", SessionID: session.ID, Agent: "lead"}
			result, err := s.approve(ex, approval.ID, allow)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.store.get("sessions", session.ID, &session); err != nil {
				t.Fatal(err)
			}
			if !result.ReceiptRecorded || session.ActiveRun != "" || len(session.Messages) != 1 {
				t.Fatalf("receipt missing or session still busy: %+v", session)
			}
			if !strings.Contains(session.Messages[0].Content, result.Status) || !strings.Contains(session.Messages[0].Content, approval.ID) {
				t.Fatal("next model turn cannot observe approval outcome")
			}
			if _, err := s.approve(ex, approval.ID, allow); err == nil {
				t.Fatal("resolved approval replayed")
			}
			notes, err := records[Memory](s.store, "notes")
			if err != nil || (allow && len(notes) != 1) || (!allow && len(notes) != 0) {
				t.Fatalf("unexpected side effects: %v %+v", err, notes)
			}
		})
	}
}

func TestProxyHTTPSRequiresTrustedPeer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name   string
		peer   string
		proto  string
		tls    bool
		want   int
		secure bool
	}{
		{"trusted_https", "172.18.0.2:1234", "https", false, 200, true},
		{"trusted_http", "172.18.0.2:1234", "http", false, 403, false},
		{"untrusted_spoof", "8.8.8.8:1234", "https", false, 403, false},
		{"ambiguous_header", "172.18.0.2:1234", "https,http", false, 403, false},
		{"direct_tls", "8.8.8.8:1234", "", true, 200, true},
		{"ssh_tunnel", "127.0.0.1:1234", "", false, 200, false},
		{"loopback_proxy_http", "127.0.0.1:1234", "http", false, 403, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := reliabilityConfig(t)
			cfg.OwnerToken = "owner-token"
			cfg.TrustedProxies = []string{"172.18.0.2", "127.0.0.1"}
			s, err := NewService(cfg, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = s.Close() })
			r := gin.New()
			s.Register(r)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/info", nil)
			req.RemoteAddr = test.peer
			req.Header.Set("Authorization", "Bearer owner-token")
			if test.proto != "" {
				req.Header.Set("X-Forwarded-Proto", test.proto)
			}
			if test.tls {
				req.TLS = &tls.ConnectionState{}
			}
			recorder := httptest.NewRecorder()
			r.ServeHTTP(recorder, req)
			cookies := recorder.Result().Cookies()
			if recorder.Code != test.want || len(cookies) != 1 || cookies[0].Secure != test.secure {
				t.Fatalf("status=%d cookies=%+v", recorder.Code, cookies)
			}
		})
	}
}

func TestIndexesAndRetentionProtectLiveAndDurableRecords(t *testing.T) {
	s := reliabilityService(t, nil)
	now := time.Now().UTC()
	session, err := s.createSession("visitor")
	if err != nil {
		t.Fatal(err)
	}
	artifactID := newID()
	session.Messages = []Message{{Role: "user", Content: "artifact_read " + artifactID}}
	if err := s.store.put("sessions", session.ID, session); err != nil {
		t.Fatal(err)
	}
	for id, record := range map[string]Memory{
		artifactID: {ID: artifactID, Principal: "visitor", Content: "retained"},
		"stale":    {ID: "stale", Principal: "other", Content: "expired"},
	} {
		if err := s.store.put("artifacts", id, record); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.store.put("memory", "durable", Memory{ID: "durable", Principal: "visitor", Content: "keep"}); err != nil {
		t.Fatal(err)
	}
	job := Job{ID: "old-job", Principal: "owner", Status: "active", SessionID: "job-session"}
	if err := s.store.put("jobs", job.ID, job); err != nil {
		t.Fatal(err)
	}
	if err := s.store.put("sessions", "job-session", Session{ID: "job-session", Principal: "owner"}); err != nil {
		t.Fatal(err)
	}
	if err := s.store.put("limits", "2020-01-01:chat:global", 10); err != nil {
		t.Fatal(err)
	}
	// Age selected records without waiting for wall clock time.
	if err := s.store.db.Update(func(tx *bolt.Tx) error {
		for bucket, ids := range map[string][]string{
			"artifacts": {artifactID, "stale"}, "memory": {"durable"},
			"jobs": {job.ID}, "sessions": {"job-session"},
		} {
			for _, id := range ids {
				if err := tx.Bucket([]byte(bucket+".updated")).Put([]byte(id), []byte(now.AddDate(0, 0, -100).Format(time.RFC3339Nano))); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.store.prune(now, 90); err != nil {
		t.Fatal(err)
	}
	for bucket, id := range map[string]string{"artifacts": artifactID, "memory": "durable", "jobs": job.ID, "sessions": "job-session"} {
		var record map[string]any
		if err := s.store.get(bucket, id, &record); err != nil {
			t.Fatalf("protected %s/%s removed: %v", bucket, id, err)
		}
	}
	var stale Memory
	if err := s.store.get("artifacts", "stale", &stale); err != ErrNotFound {
		t.Fatalf("stale artifact retained: %v", err)
	}
	var count int
	if err := s.store.get("limits", "2020-01-01:chat:global", &count); err != ErrNotFound {
		t.Fatal("old quota counter retained")
	}
	items, err := indexedRecords[Memory](s.store, "artifacts", "principal", "other")
	if err != nil || len(items) != 0 {
		t.Fatalf("deleted secondary index retained: %v %+v", err, items)
	}
	job.Status = "cancelled"
	if err := s.store.put("jobs", job.ID, job); err != nil {
		t.Fatal(err)
	}
	active, err := indexedRecords[Job](s.store, "jobs", "status", "active")
	if err != nil || len(active) != 0 {
		t.Fatalf("status index not updated atomically: %v %+v", err, active)
	}
}

func TestLegacyDatabaseBuildsIndexesOnOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte("jobs"))
		if err != nil {
			return err
		}
		return bucket.Put([]byte("legacy"), []byte(`{"id":"legacy","principal":"owner","status":"active"}`))
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	jobs, err := indexedRecords[Job](store, "jobs", "status", "active")
	if err != nil || len(jobs) != 1 || jobs[0].ID != "legacy" {
		t.Fatalf("legacy active job lost during migration: %+v %v", jobs, err)
	}
	if err := store.prune(time.Now(), 90); err != nil {
		t.Fatal(err)
	}
	jobs, err = indexedRecords[Job](store, "jobs", "principal", "owner")
	if err != nil || len(jobs) != 1 {
		t.Fatalf("migration prematurely removed legacy data: %+v %v", jobs, err)
	}
}
