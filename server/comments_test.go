package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/pasteai/pasteai/server"
)

// ── In-memory CommentStore ─────────────────────────────────

type memCommentStore struct {
	*memStore
	mu       sync.Mutex
	comments map[string]*server.Comment // key: docID+"/"+commentID
	seq      int
}

func newMemCommentStore() *memCommentStore {
	return &memCommentStore{
		memStore: newMemStore(),
		comments: make(map[string]*server.Comment),
	}
}

func (m *memCommentStore) key(docID, commentID string) string {
	return docID + "/" + commentID
}

func (m *memCommentStore) AddComment(_ context.Context, c server.Comment) (*server.Comment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	c.ID = fmt.Sprintf("comment-%d", m.seq)
	c.CreatedAt = time.Now().UTC()
	cp := c
	m.comments[m.key(c.DocID, c.ID)] = &cp
	return &cp, nil
}

func (m *memCommentStore) ListComments(_ context.Context, docID string) ([]server.Comment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []server.Comment
	for k, c := range m.comments {
		if len(k) > len(docID) && k[:len(docID)] == docID {
			out = append(out, *c)
		}
	}
	// sort by CreatedAt ascending (stable for test assertions)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].CreatedAt.Before(out[j-1].CreatedAt); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, nil
}

func (m *memCommentStore) GetComment(_ context.Context, docID, commentID string) (*server.Comment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.comments[m.key(docID, commentID)]
	if !ok {
		return nil, server.ErrNotFound
	}
	cp := *c
	return &cp, nil
}

func (m *memCommentStore) ResolveComment(_ context.Context, docID, commentID string, resolved bool) (*server.Comment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.comments[m.key(docID, commentID)]
	if !ok {
		return nil, server.ErrNotFound
	}
	c.Resolved = resolved
	cp := *c
	return &cp, nil
}

func (m *memCommentStore) DeleteComment(_ context.Context, docID, commentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := m.key(docID, commentID)
	if _, ok := m.comments[k]; !ok {
		return server.ErrNotFound
	}
	delete(m.comments, k)
	return nil
}

// ── Test helpers ───────────────────────────────────────────

func newCommentTestServer(t *testing.T) (*httptest.Server, *testCommentDB) {
	t.Helper()
	cs := newMemCommentStore()
	content := newMemContent()
	db := &testCommentDB{store: cs, content: content}
	handler := server.NewServer(cs, content, server.Options{
		Logger: log.New(io.Discard, "", 0),
	})
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts, db
}

type testCommentDB struct {
	store   *memCommentStore
	content *memContent
}

func (db *testCommentDB) createDoc(ctx context.Context, doc server.Document) (*server.Document, error) {
	raw := doc.Content
	created, err := db.store.Create(ctx, doc)
	if err != nil {
		return nil, err
	}
	if raw != "" {
		if err := db.content.Put(ctx, created.ID, []byte(raw)); err != nil {
			return nil, err
		}
		created.Content = raw
	}
	return created, nil
}

func postComment(t *testing.T, ts *httptest.Server, docID string, body map[string]any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := ts.Client().Post(ts.URL+"/api/documents/"+docID+"/comments", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST /api/documents/%s/comments: %v", docID, err)
	}
	return resp
}

func getComments(t *testing.T, ts *httptest.Server, docID string) *http.Response {
	t.Helper()
	resp, err := ts.Client().Get(ts.URL + "/api/documents/" + docID + "/comments")
	if err != nil {
		t.Fatalf("GET /api/documents/%s/comments: %v", docID, err)
	}
	return resp
}

func patchComment(t *testing.T, ts *httptest.Server, docID, commentID string, body map[string]any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPatch, ts.URL+"/api/documents/"+docID+"/comments/"+commentID, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("PATCH /api/documents/%s/comments/%s: %v", docID, commentID, err)
	}
	return resp
}

func deleteComment(t *testing.T, ts *httptest.Server, docID, commentID string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/documents/"+docID+"/comments/"+commentID, nil)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/documents/%s/comments/%s: %v", docID, commentID, err)
	}
	return resp
}

func decodeComment(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("decode comment response: %v", err)
	}
	return m
}

func decodeComments(t *testing.T, resp *http.Response) []map[string]any {
	t.Helper()
	defer resp.Body.Close()
	var s []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatalf("decode comments response: %v", err)
	}
	return s
}

var validComment = map[string]any{
	"author":      "Alice",
	"body":        "This needs clarification.",
	"start_char":  10,
	"end_char":    30,
	"quoted_text": "some selected text",
}

// ── Create comment ─────────────────────────────────────────

func TestCreateComment_Success(t *testing.T) {
	ts, db := newCommentTestServer(t)
	doc, _ := db.createDoc(context.Background(), server.Document{
		Title:      "Test",
		Content:    "Hello world some selected text here",
		Visibility: server.VisibilityPublic,
	})

	resp := postComment(t, ts, doc.ID, validComment)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	got := decodeComment(t, resp)
	if got["body"] != "This needs clarification." {
		t.Errorf("body mismatch: %v", got["body"])
	}
	if got["author"] != "Alice" {
		t.Errorf("author mismatch: %v", got["author"])
	}
	if got["quoted_text"] != "some selected text" {
		t.Errorf("quoted_text mismatch: %v", got["quoted_text"])
	}
	if got["resolved"] != false {
		t.Errorf("resolved should be false, got %v", got["resolved"])
	}
	if got["id"] == nil || got["id"] == "" {
		t.Error("expected non-empty id")
	}
	// owner_id must not be exposed
	if _, ok := got["owner_id"]; ok {
		t.Error("owner_id must not appear in response")
	}
}

func TestCreateComment_MissingBody(t *testing.T) {
	ts, db := newCommentTestServer(t)
	doc, _ := db.createDoc(context.Background(), server.Document{Title: "T", Content: "x", Visibility: server.VisibilityPublic})

	resp := postComment(t, ts, doc.ID, map[string]any{
		"quoted_text": "x", "start_char": 0, "end_char": 1,
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCreateComment_MissingQuotedText(t *testing.T) {
	ts, db := newCommentTestServer(t)
	doc, _ := db.createDoc(context.Background(), server.Document{Title: "T", Content: "x", Visibility: server.VisibilityPublic})

	resp := postComment(t, ts, doc.ID, map[string]any{
		"body": "hi", "start_char": 0, "end_char": 1,
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCreateComment_StartNotLessThanEnd(t *testing.T) {
	ts, db := newCommentTestServer(t)
	doc, _ := db.createDoc(context.Background(), server.Document{Title: "T", Content: "x", Visibility: server.VisibilityPublic})

	for _, tc := range []map[string]any{
		{"body": "b", "quoted_text": "q", "start_char": 5, "end_char": 5},  // equal
		{"body": "b", "quoted_text": "q", "start_char": 10, "end_char": 5}, // start > end
	} {
		resp := postComment(t, ts, doc.ID, tc)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for %v, got %d", tc, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

func TestCreateComment_DocNotFound(t *testing.T) {
	ts, _ := newCommentTestServer(t)
	resp := postComment(t, ts, "no-such-doc", validComment)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestCreateComment_PrivateDocForbidden(t *testing.T) {
	ts, db := newCommentTestServer(t)
	doc, _ := db.createDoc(context.Background(), server.Document{
		Title:      "Private",
		Content:    "secret",
		Visibility: server.VisibilityPrivate,
		OwnerID:    "owner-1",
	})

	// no auth — anonymous request
	resp := postComment(t, ts, doc.ID, validComment)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestCreateComment_InvalidJSON(t *testing.T) {
	ts, db := newCommentTestServer(t)
	doc, _ := db.createDoc(context.Background(), server.Document{Title: "T", Content: "x", Visibility: server.VisibilityPublic})

	resp, err := ts.Client().Post(ts.URL+"/api/documents/"+doc.ID+"/comments", "application/json", bytes.NewBufferString("{bad json"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// ── List comments ──────────────────────────────────────────

func TestListComments_Success(t *testing.T) {
	ts, db := newCommentTestServer(t)
	doc, _ := db.createDoc(context.Background(), server.Document{Title: "T", Content: "hello world", Visibility: server.VisibilityPublic})

	postComment(t, ts, doc.ID, validComment).Body.Close()
	postComment(t, ts, doc.ID, map[string]any{
		"body": "second", "quoted_text": "world", "start_char": 6, "end_char": 11,
	}).Body.Close()

	resp := getComments(t, ts, doc.ID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	comments := decodeComments(t, resp)
	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(comments))
	}
}

func TestListComments_Empty(t *testing.T) {
	ts, db := newCommentTestServer(t)
	doc, _ := db.createDoc(context.Background(), server.Document{Title: "T", Content: "x", Visibility: server.VisibilityPublic})

	resp := getComments(t, ts, doc.ID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	comments := decodeComments(t, resp)
	if len(comments) != 0 {
		t.Fatalf("expected 0 comments, got %d", len(comments))
	}
}

func TestListComments_PrivateDocForbidden(t *testing.T) {
	ts, db := newCommentTestServer(t)
	doc, _ := db.createDoc(context.Background(), server.Document{
		Title: "P", Content: "x", Visibility: server.VisibilityPrivate, OwnerID: "owner-1",
	})

	resp := getComments(t, ts, doc.ID)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestListComments_DocNotFound(t *testing.T) {
	ts, _ := newCommentTestServer(t)
	resp := getComments(t, ts, "ghost")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// ── Resolve comment ────────────────────────────────────────

func TestResolveComment_Success(t *testing.T) {
	ts, db := newCommentTestServer(t)
	doc, _ := db.createDoc(context.Background(), server.Document{Title: "T", Content: "x", Visibility: server.VisibilityPublic})
	created := decodeComment(t, postComment(t, ts, doc.ID, validComment))
	cid := created["id"].(string)

	// resolve
	resp := patchComment(t, ts, doc.ID, cid, map[string]any{"resolved": true})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	got := decodeComment(t, resp)
	if got["resolved"] != true {
		t.Errorf("expected resolved=true, got %v", got["resolved"])
	}

	// unresolve
	resp2 := patchComment(t, ts, doc.ID, cid, map[string]any{"resolved": false})
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp2.StatusCode)
	}
	got2 := decodeComment(t, resp2)
	if got2["resolved"] != false {
		t.Errorf("expected resolved=false, got %v", got2["resolved"])
	}
}

func TestResolveComment_NotFound(t *testing.T) {
	ts, db := newCommentTestServer(t)
	doc, _ := db.createDoc(context.Background(), server.Document{Title: "T", Content: "x", Visibility: server.VisibilityPublic})

	resp := patchComment(t, ts, doc.ID, "no-comment", map[string]any{"resolved": true})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestResolveComment_ForbiddenWrongOwner(t *testing.T) {
	// Build a server with auth so ownership is enforced.
	cs := newMemCommentStore()
	content := newMemContent()
	auth := &staticOwnerAuth{ownerID: "user-b"}
	handler := server.NewServer(cs, content, server.Options{
		Logger:              log.New(io.Discard, "", 0),
		AuthProvider:        auth,
		AllowAnonymousWrites: true,
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	// Create doc owned by user-a
	doc, _ := cs.Create(context.Background(), server.Document{
		Title: "T", Visibility: server.VisibilityPublic, OwnerID: "user-a",
	})
	content.Put(context.Background(), doc.ID, []byte("hello"))

	// Add comment owned by user-a (inject ownerID directly into store)
	c, _ := cs.AddComment(context.Background(), server.Comment{
		DocID: doc.ID, OwnerID: "user-a",
		Body: "hi", QuotedText: "hello", StartChar: 0, EndChar: 5,
	})

	// user-b tries to resolve user-a's comment on user-a's doc
	resp := patchComment(t, ts, doc.ID, c.ID, map[string]any{"resolved": true})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// ── Delete comment ─────────────────────────────────────────

func TestDeleteComment_Success(t *testing.T) {
	ts, db := newCommentTestServer(t)
	doc, _ := db.createDoc(context.Background(), server.Document{Title: "T", Content: "x", Visibility: server.VisibilityPublic})
	created := decodeComment(t, postComment(t, ts, doc.ID, validComment))
	cid := created["id"].(string)

	resp := deleteComment(t, ts, doc.ID, cid)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// confirm gone
	listResp := getComments(t, ts, doc.ID)
	comments := decodeComments(t, listResp)
	if len(comments) != 0 {
		t.Errorf("expected 0 comments after delete, got %d", len(comments))
	}
}

func TestDeleteComment_NotFound(t *testing.T) {
	ts, db := newCommentTestServer(t)
	doc, _ := db.createDoc(context.Background(), server.Document{Title: "T", Content: "x", Visibility: server.VisibilityPublic})

	resp := deleteComment(t, ts, doc.ID, "ghost")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestDeleteComment_ForbiddenWrongOwner(t *testing.T) {
	cs := newMemCommentStore()
	content := newMemContent()
	auth := &staticOwnerAuth{ownerID: "user-b"}
	handler := server.NewServer(cs, content, server.Options{
		Logger:              log.New(io.Discard, "", 0),
		AuthProvider:        auth,
		AllowAnonymousWrites: true,
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	doc, _ := cs.Create(context.Background(), server.Document{
		Title: "T", Visibility: server.VisibilityPublic, OwnerID: "user-a",
	})
	content.Put(context.Background(), doc.ID, []byte("hello"))

	c, _ := cs.AddComment(context.Background(), server.Comment{
		DocID: doc.ID, OwnerID: "user-a",
		Body: "hi", QuotedText: "hello", StartChar: 0, EndChar: 5,
	})

	resp := deleteComment(t, ts, doc.ID, c.ID)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// ── Routes not registered without CommentStore ─────────────

func TestCommentRoutes_NotRegisteredWithoutCommentStore(t *testing.T) {
	// newTestServer uses plain memStore which does not implement CommentStore.
	ts, db := newTestServer(t)
	doc, _ := db.Create(context.Background(), server.Document{
		Title: "T", Content: "x", Visibility: server.VisibilityPublic,
	})

	resp, err := ts.Client().Get(ts.URL + "/api/documents/" + doc.ID + "/comments")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 (route not registered), got %d", resp.StatusCode)
	}
}

// ── Ownership: doc owner can modify another user's comment ──

func TestResolveComment_DocOwnerCanModify(t *testing.T) {
	cs := newMemCommentStore()
	content := newMemContent()
	// doc owner is user-a; request comes from user-a
	auth := &staticOwnerAuth{ownerID: "user-a"}
	handler := server.NewServer(cs, content, server.Options{
		Logger:              log.New(io.Discard, "", 0),
		AuthProvider:        auth,
		AllowAnonymousWrites: true,
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	doc, _ := cs.Create(context.Background(), server.Document{
		Title: "T", Visibility: server.VisibilityPublic, OwnerID: "user-a",
	})
	content.Put(context.Background(), doc.ID, []byte("hello"))

	// comment belongs to user-b, but user-a (doc owner) resolves it
	c, _ := cs.AddComment(context.Background(), server.Comment{
		DocID: doc.ID, OwnerID: "user-b",
		Body: "hi", QuotedText: "hello", StartChar: 0, EndChar: 5,
	})

	resp := patchComment(t, ts, doc.ID, c.ID, map[string]any{"resolved": true})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	got := decodeComment(t, resp)
	if got["resolved"] != true {
		t.Errorf("expected resolved=true, got %v", got["resolved"])
	}
}

// staticOwnerAuth always returns the same ownerID for any authenticated request.
type staticOwnerAuth struct{ ownerID string }

func (a *staticOwnerAuth) Authenticate(_ *http.Request) (string, error) {
	return a.ownerID, nil
}
