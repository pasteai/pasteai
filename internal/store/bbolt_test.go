package store_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pasteai/pasteai/internal/store"
)

func newTestStore(t *testing.T) store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.NewBolt(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewBolt: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	doc, err := s.Create(ctx, store.Document{
		Title:   "Hello",
		Content: "# Hello\n\nWorld",
		Author:  "Claude",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if doc.ID == "" {
		t.Error("expected non-empty ID")
	}
	if doc.Title != "Hello" {
		t.Errorf("title = %q, want %q", doc.Title, "Hello")
	}
	if doc.Visibility != store.VisibilityPublic {
		t.Errorf("default visibility = %q, want %q", doc.Visibility, store.VisibilityPublic)
	}
	if doc.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
}

func TestCreateDefaultsVisibility(t *testing.T) {
	s := newTestStore(t)
	doc, err := s.Create(context.Background(), store.Document{
		Title:   "Test",
		Content: "body",
	})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Visibility != store.VisibilityPublic {
		t.Errorf("got visibility %q, want public", doc.Visibility)
	}
}

func TestGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	created, _ := s.Create(ctx, store.Document{Title: "A", Content: "body"})

	got, err := s.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID mismatch: got %q, want %q", got.ID, created.ID)
	}
	if got.Content != "body" {
		t.Errorf("Content = %q, want %q", got.Content, "body")
	}
}

func TestGetNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Get(context.Background(), "nonexistent-id")
	if err != store.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestGetContentMissing(t *testing.T) {
	dir := t.TempDir()
	s, err := store.NewBolt(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewBolt: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	doc, err := s.Create(context.Background(), store.Document{Title: "X", Content: "body"})
	if err != nil {
		t.Fatal(err)
	}

	// Delete the content file behind the store's back.
	if err := os.Remove(filepath.Join(dir, "documents", doc.ID+".md")); err != nil {
		t.Fatal(err)
	}

	_, err = s.Get(context.Background(), doc.ID)
	if !errors.Is(err, store.ErrContentMissing) {
		t.Errorf("expected ErrContentMissing, got %v", err)
	}
}

func TestListEmpty(t *testing.T) {
	s := newTestStore(t)
	result, err := s.List(context.Background(), store.ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(result.Documents) != 0 {
		t.Errorf("expected 0 docs, got %d", len(result.Documents))
	}
}

func TestListNewestFirst(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	titles := []string{"First", "Second", "Third"}
	for _, title := range titles {
		s.Create(ctx, store.Document{Title: title, Content: "x"})
		// Small sleep to ensure distinct timestamps
		time.Sleep(2 * time.Millisecond)
	}

	result, err := s.List(ctx, store.ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(result.Documents) != 3 {
		t.Fatalf("expected 3 docs, got %d", len(result.Documents))
	}
	// Newest first
	if result.Documents[0].Title != "Third" {
		t.Errorf("first doc = %q, want Third", result.Documents[0].Title)
	}
	if result.Documents[2].Title != "First" {
		t.Errorf("last doc = %q, want First", result.Documents[2].Title)
	}
}

func TestListLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		s.Create(ctx, store.Document{Title: "Doc", Content: "x"})
	}

	result, err := s.List(ctx, store.ListOptions{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Documents) != 3 {
		t.Errorf("expected 3 docs with limit 3, got %d", len(result.Documents))
	}
}

func TestListPagination(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Create 5 documents with distinct timestamps
	for i := 0; i < 5; i++ {
		s.Create(ctx, store.Document{Title: fmt.Sprintf("Doc %d", i+1), Content: "x"})
		time.Sleep(2 * time.Millisecond)
	}

	// Page 1: limit 3
	page1, err := s.List(ctx, store.ListOptions{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(page1.Documents) != 3 {
		t.Fatalf("page1: expected 3 docs, got %d", len(page1.Documents))
	}
	if page1.NextToken == "" {
		t.Fatal("page1: expected NextToken, got empty")
	}
	// Newest first: Doc 5, Doc 4, Doc 3
	if page1.Documents[0].Title != "Doc 5" {
		t.Errorf("page1[0] = %q, want Doc 5", page1.Documents[0].Title)
	}

	// Page 2: use cursor
	page2, err := s.List(ctx, store.ListOptions{Limit: 3, NextToken: page1.NextToken})
	if err != nil {
		t.Fatal(err)
	}
	if len(page2.Documents) != 2 {
		t.Fatalf("page2: expected 2 docs, got %d", len(page2.Documents))
	}
	if page2.NextToken != "" {
		t.Errorf("page2: expected empty NextToken (last page), got %q", page2.NextToken)
	}
	// Continuing newest-first: Doc 2, Doc 1
	if page2.Documents[0].Title != "Doc 2" {
		t.Errorf("page2[0] = %q, want Doc 2", page2.Documents[0].Title)
	}

	// Verify no overlap between pages
	ids1 := map[string]bool{}
	for _, d := range page1.Documents {
		ids1[d.ID] = true
	}
	for _, d := range page2.Documents {
		if ids1[d.ID] {
			t.Errorf("document %q appears in both pages (cursor is broken)", d.ID)
		}
	}
}

func TestListPaginationInvalidCursor(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	s.Create(ctx, store.Document{Title: "Doc", Content: "x"})

	// An invalid cursor should fall back to the first page gracefully.
	result, err := s.List(ctx, store.ListOptions{Limit: 10, NextToken: "not-valid-hex!!"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Documents) != 1 {
		t.Errorf("expected 1 doc with invalid cursor, got %d", len(result.Documents))
	}
}

func TestListOnlyPublic(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.Create(ctx, store.Document{Title: "Public", Content: "x", Visibility: store.VisibilityPublic})
	s.Create(ctx, store.Document{Title: "Unlisted", Content: "x", Visibility: store.VisibilityUnlisted})
	s.Create(ctx, store.Document{Title: "Private", Content: "x", Visibility: store.VisibilityPrivate})

	result, err := s.List(ctx, store.ListOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Documents) != 1 {
		t.Errorf("expected 1 public doc, got %d", len(result.Documents))
	}
	if result.Documents[0].Title != "Public" {
		t.Errorf("expected Public, got %q", result.Documents[0].Title)
	}
}

func TestListByOwner(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.Create(ctx, store.Document{Title: "Alice doc", Content: "x", OwnerID: "alice", Visibility: store.VisibilityPrivate})
	s.Create(ctx, store.Document{Title: "Bob doc", Content: "x", OwnerID: "bob", Visibility: store.VisibilityPrivate})

	result, err := s.List(ctx, store.ListOptions{OwnerID: "alice", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Documents) != 1 || result.Documents[0].Title != "Alice doc" {
		t.Errorf("expected Alice doc only, got %v", result.Documents)
	}
}

func TestPersistence(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "persist.db")

	s1, _ := store.NewBolt(dbPath)
	created, _ := s1.Create(context.Background(), store.Document{Title: "Persisted", Content: "data"})
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

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
