package store_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pasteai/pasteai/server"
	"github.com/pasteai/pasteai/store"
)

// ── Helpers ────────────────────────────────────────────────

func newTestBolt(t *testing.T) *store.BoltStore {
	t.Helper()
	dir := t.TempDir()
	s, err := store.NewBolt(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewBolt: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func newTestDisk(t *testing.T) *store.DiskContent {
	t.Helper()
	dir := t.TempDir()
	c, err := store.NewDiskContent(dir)
	if err != nil {
		t.Fatalf("NewDiskContent: %v", err)
	}
	return c
}

// ── BoltStore tests ────────────────────────────────────────

func TestBoltCreate(t *testing.T) {
	s := newTestBolt(t)
	ctx := context.Background()

	doc, err := s.Create(ctx, server.Document{
		Title:  "Hello",
		Author: "Claude",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if doc.ID == "" {
		t.Error("expected non-empty ID")
	}
	if doc.Title != "Hello" {
		t.Errorf("title = %q, want Hello", doc.Title)
	}
	if doc.Visibility != server.VisibilityPublic {
		t.Errorf("default visibility = %q, want public", doc.Visibility)
	}
	if doc.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
	if doc.Content != "" {
		t.Error("BoltStore.Create must return empty Content; content is managed by ContentBackend")
	}
}

func TestBoltGet(t *testing.T) {
	s := newTestBolt(t)
	ctx := context.Background()

	created, _ := s.Create(ctx, server.Document{Title: "A"})

	got, err := s.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID mismatch: got %q, want %q", got.ID, created.ID)
	}
	if got.Content != "" {
		t.Error("BoltStore.Get must return empty Content")
	}
}

func TestBoltGetNotFound(t *testing.T) {
	s := newTestBolt(t)
	_, err := s.Get(context.Background(), "nonexistent-id")
	if !errors.Is(err, server.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestBoltUpdateTitle(t *testing.T) {
	s := newTestBolt(t)
	ctx := context.Background()
	doc, _ := s.Create(ctx, server.Document{Title: "Original"})

	updated, err := s.Update(ctx, doc.ID, "New Title")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Title != "New Title" {
		t.Errorf("title = %q, want New Title", updated.Title)
	}
	if updated.Content != "" {
		t.Error("Update must return empty Content")
	}

	got, _ := s.Get(ctx, doc.ID)
	if got.Title != "New Title" {
		t.Errorf("persisted title = %q, want New Title", got.Title)
	}
}

func TestBoltUpdateNotFound(t *testing.T) {
	s := newTestBolt(t)
	_, err := s.Update(context.Background(), "no-such-id", "x")
	if !errors.Is(err, server.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestBoltDelete(t *testing.T) {
	s := newTestBolt(t)
	ctx := context.Background()
	doc, _ := s.Create(ctx, server.Document{Title: "Delete Me"})

	if err := s.Delete(ctx, doc.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := s.Get(ctx, doc.ID)
	if !errors.Is(err, server.ErrNotFound) {
		t.Errorf("expected ErrNotFound after Delete, got %v", err)
	}
}

func TestBoltDeleteNotFound(t *testing.T) {
	s := newTestBolt(t)
	err := s.Delete(context.Background(), "no-such-id")
	if !errors.Is(err, server.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestBoltListEmpty(t *testing.T) {
	s := newTestBolt(t)
	result, err := s.List(context.Background(), server.ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(result.Documents) != 0 {
		t.Errorf("expected 0 docs, got %d", len(result.Documents))
	}
}

func TestBoltListNewestFirst(t *testing.T) {
	s := newTestBolt(t)
	ctx := context.Background()
	for _, title := range []string{"First", "Second", "Third"} {
		s.Create(ctx, server.Document{Title: title})
		time.Sleep(2 * time.Millisecond)
	}
	result, err := s.List(ctx, server.ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(result.Documents) != 3 {
		t.Fatalf("expected 3 docs, got %d", len(result.Documents))
	}
	if result.Documents[0].Title != "Third" {
		t.Errorf("first doc = %q, want Third", result.Documents[0].Title)
	}
	if result.Documents[2].Title != "First" {
		t.Errorf("last doc = %q, want First", result.Documents[2].Title)
	}
}

func TestBoltListLimit(t *testing.T) {
	s := newTestBolt(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		s.Create(ctx, server.Document{Title: "Doc"})
	}
	result, err := s.List(ctx, server.ListOptions{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Documents) != 3 {
		t.Errorf("expected 3 docs, got %d", len(result.Documents))
	}
}

func TestBoltListPagination(t *testing.T) {
	s := newTestBolt(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		s.Create(ctx, server.Document{Title: fmt.Sprintf("Doc %d", i+1)})
		time.Sleep(2 * time.Millisecond)
	}

	page1, err := s.List(ctx, server.ListOptions{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(page1.Documents) != 3 {
		t.Fatalf("page1: expected 3 docs, got %d", len(page1.Documents))
	}
	if page1.NextToken == "" {
		t.Fatal("page1: expected NextToken")
	}
	if page1.Documents[0].Title != "Doc 5" {
		t.Errorf("page1[0] = %q, want Doc 5", page1.Documents[0].Title)
	}

	page2, err := s.List(ctx, server.ListOptions{Limit: 3, NextToken: page1.NextToken})
	if err != nil {
		t.Fatal(err)
	}
	if len(page2.Documents) != 2 {
		t.Fatalf("page2: expected 2 docs, got %d", len(page2.Documents))
	}
	if page2.NextToken != "" {
		t.Errorf("page2: expected empty NextToken (last page), got %q", page2.NextToken)
	}
	if page2.Documents[0].Title != "Doc 2" {
		t.Errorf("page2[0] = %q, want Doc 2", page2.Documents[0].Title)
	}

	ids1 := map[string]bool{}
	for _, d := range page1.Documents {
		ids1[d.ID] = true
	}
	for _, d := range page2.Documents {
		if ids1[d.ID] {
			t.Errorf("document %q appears in both pages", d.ID)
		}
	}
}

func TestBoltListInvalidCursor(t *testing.T) {
	s := newTestBolt(t)
	ctx := context.Background()
	s.Create(ctx, server.Document{Title: "Doc"})

	result, err := s.List(ctx, server.ListOptions{Limit: 10, NextToken: "not-valid-hex!!"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Documents) != 1 {
		t.Errorf("expected 1 doc with invalid cursor, got %d", len(result.Documents))
	}
}

func TestBoltListOnlyPublic(t *testing.T) {
	s := newTestBolt(t)
	ctx := context.Background()
	s.Create(ctx, server.Document{Title: "Public", Visibility: server.VisibilityPublic})
	s.Create(ctx, server.Document{Title: "Unlisted", Visibility: server.VisibilityUnlisted})
	s.Create(ctx, server.Document{Title: "Private", Visibility: server.VisibilityPrivate})

	result, err := s.List(ctx, server.ListOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Documents) != 1 || result.Documents[0].Title != "Public" {
		t.Errorf("expected only Public doc, got %v", result.Documents)
	}
}

func TestBoltListByOwner(t *testing.T) {
	s := newTestBolt(t)
	ctx := context.Background()
	s.Create(ctx, server.Document{Title: "Alice doc", OwnerID: "alice", Visibility: server.VisibilityPrivate})
	s.Create(ctx, server.Document{Title: "Bob doc", OwnerID: "bob", Visibility: server.VisibilityPrivate})

	result, err := s.List(ctx, server.ListOptions{OwnerID: "alice", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Documents) != 1 || result.Documents[0].Title != "Alice doc" {
		t.Errorf("expected Alice doc only, got %v", result.Documents)
	}
}

func TestBoltPersistence(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "persist.db")

	s1, _ := store.NewBolt(dbPath)
	created, _ := s1.Create(context.Background(), server.Document{Title: "Persisted"})
	s1.Close()

	s2, _ := store.NewBolt(dbPath)
	defer s2.Close()

	got, err := s2.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if got.Title != "Persisted" {
		t.Errorf("title after reopen = %q, want Persisted", got.Title)
	}
}

// ── DiskContent tests ──────────────────────────────────────

func TestDiskPutGet(t *testing.T) {
	c := newTestDisk(t)
	ctx := context.Background()

	if err := c.Put(ctx, "abc", []byte("hello world")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := c.Get(ctx, "abc")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "hello world" {
		t.Errorf("got %q, want %q", got, "hello world")
	}
}

func TestDiskGetNotFound(t *testing.T) {
	c := newTestDisk(t)
	_, err := c.Get(context.Background(), "no-such-id")
	if !errors.Is(err, server.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDiskDelete(t *testing.T) {
	c := newTestDisk(t)
	ctx := context.Background()
	c.Put(ctx, "abc", []byte("content"))

	if err := c.Delete(ctx, "abc"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := c.Get(ctx, "abc")
	if !errors.Is(err, server.ErrNotFound) {
		t.Errorf("expected ErrNotFound after Delete, got %v", err)
	}
}

func TestDiskDeleteMissing(t *testing.T) {
	c := newTestDisk(t)
	if err := c.Delete(context.Background(), "no-such-id"); err != nil {
		t.Errorf("Delete of missing file should be a no-op, got %v", err)
	}
}

func TestDiskPutOverwrites(t *testing.T) {
	c := newTestDisk(t)
	ctx := context.Background()
	c.Put(ctx, "abc", []byte("first"))
	c.Put(ctx, "abc", []byte("second"))

	got, _ := c.Get(ctx, "abc")
	if string(got) != "second" {
		t.Errorf("got %q, want second", got)
	}
}

// ── Integration: BoltStore + DiskContent together ──────────

func TestBoltAndDiskIntegration(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	contentDir := store.DirFromDBPath(dbPath)

	s, err := store.NewBolt(dbPath)
	if err != nil {
		t.Fatalf("NewBolt: %v", err)
	}
	defer s.Close()

	c, err := store.NewDiskContent(contentDir)
	if err != nil {
		t.Fatalf("NewDiskContent: %v", err)
	}

	ctx := context.Background()

	// Create: metadata in bolt, content on disk
	doc, err := s.Create(ctx, server.Document{Title: "Integration Test"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := c.Put(ctx, doc.ID, []byte("# Hello from disk")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Get: metadata from bolt, content from disk
	meta, err := s.Get(ctx, doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	content, err := c.Get(ctx, doc.ID)
	if err != nil {
		t.Fatalf("content.Get: %v", err)
	}
	meta.Content = string(content)

	if meta.Title != "Integration Test" {
		t.Errorf("title = %q, want Integration Test", meta.Title)
	}
	if meta.Content != "# Hello from disk" {
		t.Errorf("content = %q, want '# Hello from disk'", meta.Content)
	}

	// Delete: remove from both
	if err := s.Delete(ctx, doc.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := c.Delete(ctx, doc.ID); err != nil {
		t.Fatalf("content.Delete: %v", err)
	}

	_, err = s.Get(ctx, doc.ID)
	if !errors.Is(err, server.ErrNotFound) {
		t.Errorf("expected ErrNotFound after Delete, got %v", err)
	}
	_, err = c.Get(ctx, doc.ID)
	if !errors.Is(err, server.ErrNotFound) {
		t.Errorf("expected ErrNotFound for content after Delete, got %v", err)
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
