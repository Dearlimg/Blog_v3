package article

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"blog-front/internal/user"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	err = db.AutoMigrate(&user.Entity{}, &Entity{}, &Draft{}, &Revision{}, &Asset{}, &Receipt{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&user.Entity{ID: 1, Username: "owner", Email: "owner@test.invalid"}).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func mutation(version uint, action string) Mutation {
	return Mutation{Content: Content{Title: "Original", Body: "## Answer\n\nGo", Category: "interview",
		Summary: "Summary", Tags: []string{"Go"}, PrivateNotes: "PRIVATE COMPANY"},
		Version: version, RequestID: uuid.NewString(), Action: action}
}

func mustMutate(t *testing.T, s *Service, id string, m Mutation) *Document {
	t.Helper()
	d, err := s.Mutate(1, id, m)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestPublicationIsolationAndRecovery(t *testing.T) {
	s := NewService(testDB(t))
	id := uuid.NewString()
	mustMutate(t, s, id, mutation(0, "save"))
	if _, err := s.repo.PublicGet(id); err != gorm.ErrRecordNotFound {
		t.Fatalf("draft public: %v", err)
	}
	published := mustMutate(t, s, id, mutation(1, "publish"))
	edit := mutation(2, "save")
	edit.Title = "Secret edit"
	edit.Body = "Unpublished changes"
	edit.Tags = []string{"private-tag"}
	edit.Category = "notes"
	mustMutate(t, s, id, edit)
	public, err := s.repo.PublicGet(id)
	if err != nil {
		t.Fatal(err)
	}
	if public.Title != "Original" || public.Body != "## Answer\n\nGo" || public.Category != "interview" {
		t.Fatalf("draft leaked: %+v", public)
	}
	data, _ := json.Marshal(public)
	if strings.Contains(string(data), "PRIVATE") || strings.Contains(string(data), "private_notes") {
		t.Fatal("private data leaked")
	}
	_, total, err := s.repo.PublicList(Filter{Page: 1, Tag: "private-tag"})
	if err != nil || total != 0 {
		t.Fatalf("draft tag leaked: %d %v", total, err)
	}
	restore := mutation(3, "restore")
	restore.RevisionID = published.PublishedRevisionID
	d := mustMutate(t, s, id, restore)
	if d.Draft.Title != "Original" || d.Draft.Version != 4 {
		t.Fatalf("bad restore: %+v", d)
	}
	var before Revision
	if err := s.repo.db.First(&before, "article_id = ? AND reason = ?", id, "before_restore").Error; err != nil {
		t.Fatal(err)
	}
	if before.Body != edit.Body {
		t.Fatal("restore discarded current draft")
	}
	mustMutate(t, s, id, mutation(4, "trash"))
	if _, err := s.repo.PublicGet(id); err != gorm.ErrRecordNotFound {
		t.Fatal("trash public")
	}
	d = mustMutate(t, s, id, mutation(5, "untrash"))
	if d.PublishedRevisionID != 0 || d.Trashed {
		t.Fatal("untrash must restore privately")
	}
}

func TestRetryAndConcurrentConflict(t *testing.T) {
	s := NewService(testDB(t))
	id := uuid.NewString()
	create := mutation(0, "save")
	mustMutate(t, s, id, create)
	retry := mustMutate(t, s, id, create)
	if retry.Draft.Version != 1 {
		t.Fatal("retry advanced version")
	}
	create.Body = "different payload"
	if _, err := s.Mutate(1, id, create); err != ErrConflict {
		t.Fatalf("request id reused: %v", err)
	}
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.Mutate(1, id, mutation(1, "save")); errors <- err }()
	}
	wg.Wait()
	close(errors)
	success, conflicts := 0, 0
	for err := range errors {
		switch err {
		case nil:
			success++
		case ErrConflict:
			conflicts++
		default:
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflicts)
	}
	var count int64
	s.repo.db.Model(&Entity{}).Count(&count)
	if count != 1 {
		t.Fatal("duplicate article")
	}
}

func TestAutomaticSnapshotRetention(t *testing.T) {
	s := NewService(testDB(t))
	id := uuid.NewString()
	mustMutate(t, s, id, mutation(0, "save"))
	for i := 0; i < 103; i++ {
		if err := s.repo.db.Create(&Revision{ArticleID: id, Reason: "auto", Content: Content{Tags: []string{}}}).Error; err != nil {
			t.Fatal(err)
		}
	}
	s.repo.db.Model(&Draft{}).Where("article_id = ?", id).Update("snapshot_at", time.Now().Add(-6*time.Minute))
	mustMutate(t, s, id, mutation(1, "save"))
	var count int64
	s.repo.db.Model(&Revision{}).Where("article_id = ? AND reason = ?", id, "auto").Count(&count)
	if count != 100 {
		t.Fatalf("retained %d auto snapshots", count)
	}
}

func token(t *testing.T, owner uint) string {
	t.Helper()
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"user_id": owner, "exp": time.Now().Add(time.Hour).Unix()}).SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestHTTPPermissionsAndAssets(t *testing.T) {
	db := testDB(t)
	s := NewService(db)
	id := uuid.NewString()
	mustMutate(t, s, id, mutation(0, "save"))
	assetID := uuid.NewString()
	assetURL := "/api/v1/articles/" + id + "/assets/" + assetID
	if err := db.Create(&Asset{ID: assetID, ArticleID: id, MediaType: "image/png", Data: []byte("test-image")}).Error; err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	Register(r, db, "test-secret", 1)
	request := func(method, path, auth string, body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	for _, path := range []string{"/api/v1/admin/articles", "/api/v1/admin/articles/" + id,
		"/api/v1/admin/articles/" + id + "/revisions", strings.Replace(assetURL, "/articles/", "/admin/articles/", 1)} {
		if w := request("GET", path, "", nil); w.Code != 401 {
			t.Fatalf("anonymous %s: %d", path, w.Code)
		}
		if w := request("GET", path, token(t, 2), nil); w.Code != 403 {
			t.Fatalf("reader %s: %d", path, w.Code)
		}
	}
	body, _ := json.Marshal(mutation(1, "publish"))
	if w := request("PUT", "/api/v1/admin/articles/"+id, token(t, 2), body); w.Code != 403 {
		t.Fatal("reader can mutate")
	}
	if w := request("GET", assetURL, "", nil); w.Code != 404 {
		t.Fatal("draft asset public")
	}
	m := mutation(1, "publish")
	m.Body = "![image](" + assetURL + ")"
	mustMutate(t, s, id, m)
	if w := request("GET", assetURL, "", nil); w.Code != 200 {
		t.Fatalf("published asset: %d", w.Code)
	}
	if w := request("GET", "/api/v1/articles/"+id, "", nil); strings.Contains(w.Body.String(), "PRIVATE") {
		t.Fatal("private notes leaked")
	}
	mustMutate(t, s, id, mutation(2, "unpublish"))
	if w := request("GET", assetURL, "", nil); w.Code != 404 {
		t.Fatal("unpublished asset public")
	}
	rDisabled := gin.New()
	Register(rDisabled, db, "test-secret", 0)
	req := httptest.NewRequest("GET", "/api/v1/admin/articles/access", nil)
	req.Header.Set("Authorization", "Bearer "+token(t, 1))
	w := httptest.NewRecorder()
	rDisabled.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal("unconfigured owner allowed")
	}
}

func TestValidationAndCrossArticleRestore(t *testing.T) {
	s := NewService(testDB(t))
	id := uuid.NewString()
	mustMutate(t, s, id, mutation(0, "save"))
	other := uuid.NewString()
	mustMutate(t, s, other, mutation(0, "save"))
	published := mustMutate(t, s, other, mutation(1, "publish"))
	m := mutation(1, "restore")
	m.RevisionID = published.PublishedRevisionID
	if _, err := s.Mutate(1, id, m); err != gorm.ErrRecordNotFound {
		t.Fatalf("cross article restore: %v", err)
	}
	for i, m := range []Mutation{
		{RequestID: uuid.NewString(), Action: "publish", Content: Content{Title: "", Body: "body"}},
		{RequestID: uuid.NewString(), Action: "save", Content: Content{Body: strings.Repeat("x", 2*1024*1024+1)}},
		{RequestID: "not-uuid", Action: "save"},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			if _, err := s.Mutate(1, id, m); err != ErrInvalid {
				t.Fatalf("invalid accepted: %v", err)
			}
		})
	}
}
