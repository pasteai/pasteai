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

func TestBoltSearchNoResults(t *testing.T) {
	s := newTestBolt(t)
	ctx := context.Background()
	s.Create(ctx, server.Document{Title: "Hello world", Visibility: server.VisibilityPublic})

	results, err := s.Search(ctx, server.SearchOptions{Query: "nomatch"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected no results, got %d", len(results))
	}
}

func TestBoltSearchTitleMatch(t *testing.T) {
	s := newTestBolt(t)
	ctx := context.Background()
	s.Create(ctx, server.Document{Title: "Auth flow guide", Visibility: server.VisibilityPublic})
	s.Create(ctx, server.Document{Title: "Deployment notes", Visibility: server.VisibilityPublic})

	results, err := s.Search(ctx, server.SearchOptions{Query: "AUTH"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Title != "Auth flow guide" {
		t.Errorf("expected [Auth flow guide], got %v", results)
	}
}

func TestBoltSearchVisibility(t *testing.T) {
	s := newTestBolt(t)
	ctx := context.Background()
	s.Create(ctx, server.Document{Title: "public doc", Visibility: server.VisibilityPublic})
	s.Create(ctx, server.Document{Title: "unlisted doc", OwnerID: "alice", Visibility: server.VisibilityUnlisted})

	pub, _ := s.Search(ctx, server.SearchOptions{Query: "doc"})
	if len(pub) != 1 || pub[0].Title != "public doc" {
		t.Errorf("anonymous should see public only, got %v", pub)
	}

	own, _ := s.Search(ctx, server.SearchOptions{Query: "doc", OwnerID: "alice"})
	if len(own) != 1 || own[0].Title != "unlisted doc" {
		t.Errorf("owner should see their docs, got %v", own)
	}
}

func TestBoltSearchLimit(t *testing.T) {
	s := newTestBolt(t)
	ctx := context.Background()
	for i := range 5 {
		s.Create(ctx, server.Document{Title: fmt.Sprintf("doc %d", i), Visibility: server.VisibilityPublic})
	}

	results, err := s.Search(ctx, server.SearchOptions{Query: "doc", Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
}

func TestBoltSearchNewestFirst(t *testing.T) {
	s := newTestBolt(t)
	ctx := context.Background()
	s.Create(ctx, server.Document{Title: "first doc", Visibility: server.VisibilityPublic})
	time.Sleep(time.Millisecond)
	s.Create(ctx, server.Document{Title: "second doc", Visibility: server.VisibilityPublic})

	results, err := s.Search(ctx, server.SearchOptions{Query: "doc"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Title != "second doc" {
		t.Errorf("expected newest first, got %v", results)
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

func TestBoltUpdateVisibility(t *testing.T) {
	s := newTestBolt(t)
	ctx := context.Background()
	doc, _ := s.Create(ctx, server.Document{Title: "Test", Visibility: server.VisibilityPublic})

	updated, err := s.UpdateVisibility(ctx, doc.ID, server.VisibilityUnlisted)
	if err != nil {
		t.Fatalf("UpdateVisibility: %v", err)
	}
	if updated.Visibility != server.VisibilityUnlisted {
		t.Errorf("visibility = %q, want unlisted", updated.Visibility)
	}
	if updated.Content != "" {
		t.Error("UpdateVisibility must return empty Content")
	}
	got, _ := s.Get(ctx, doc.ID)
	if got.Visibility != server.VisibilityUnlisted {
		t.Errorf("persisted visibility = %q, want unlisted", got.Visibility)
	}
}

func TestBoltUpdateVisibilityNotFound(t *testing.T) {
	s := newTestBolt(t)
	_, err := s.UpdateVisibility(context.Background(), "nonexistent", server.VisibilityUnlisted)
	if !errors.Is(err, server.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// ── BoltStore revision tests ───────────────────────────────

func makeRevision(docID string) server.Revision {
	return server.Revision{
		DocID:      docID,
		Title:      "Test",
		Author:     "alice",
		Visibility: server.VisibilityPublic,
		SavedAt:    time.Now().UTC(),
	}
}

func TestSaveAndListRevisions(t *testing.T) {
	ctx := context.Background()
	s := newTestBolt(t)

	doc, _ := s.Create(ctx, server.Document{Title: "doc"})

	rev1 := makeRevision(doc.ID)
	if err := s.SaveRevision(ctx, rev1); err != nil {
		t.Fatalf("SaveRevision: %v", err)
	}
	rev2 := makeRevision(doc.ID)
	rev2.Title = "Updated"
	if err := s.SaveRevision(ctx, rev2); err != nil {
		t.Fatalf("SaveRevision: %v", err)
	}

	revs, err := s.ListRevisions(ctx, doc.ID)
	if err != nil {
		t.Fatalf("ListRevisions: %v", err)
	}
	if len(revs) != 2 {
		t.Fatalf("want 2 revisions, got %d", len(revs))
	}
	// Newest first: rev2 (Num=2), rev1 (Num=1).
	if revs[0].Num != 2 || revs[1].Num != 1 {
		t.Errorf("order wrong: nums %d, %d", revs[0].Num, revs[1].Num)
	}
}

func TestGetRevision(t *testing.T) {
	ctx := context.Background()
	s := newTestBolt(t)

	doc, _ := s.Create(ctx, server.Document{Title: "doc"})
	rev := makeRevision(doc.ID)
	rev.AddedLines = 5
	rev.RemovedLines = 2
	_ = s.SaveRevision(ctx, rev)

	got, err := s.GetRevision(ctx, doc.ID, 1)
	if err != nil {
		t.Fatalf("GetRevision: %v", err)
	}
	if got.Num != 1 || got.AddedLines != 5 || got.RemovedLines != 2 {
		t.Errorf("got %+v", got)
	}
}

func TestGetRevisionNotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestBolt(t)
	_, err := s.GetRevision(ctx, "no-such-doc", 1)
	if !errors.Is(err, server.ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestRevisionPruning(t *testing.T) {
	ctx := context.Background()
	s := newTestBolt(t)

	doc, _ := s.Create(ctx, server.Document{Title: "doc"})
	for i := 0; i < 52; i++ {
		if err := s.SaveRevision(ctx, makeRevision(doc.ID)); err != nil {
			t.Fatalf("SaveRevision %d: %v", i, err)
		}
	}

	revs, err := s.ListRevisions(ctx, doc.ID)
	if err != nil {
		t.Fatalf("ListRevisions: %v", err)
	}
	if len(revs) != 50 {
		t.Errorf("want 50 revisions after pruning, got %d", len(revs))
	}
	// Oldest should be rev 3 (revs 1 and 2 pruned).
	if revs[len(revs)-1].Num != 3 {
		t.Errorf("want oldest Num=3 after pruning, got %d", revs[len(revs)-1].Num)
	}
}

func TestDeleteRevisions(t *testing.T) {
	ctx := context.Background()
	s := newTestBolt(t)

	doc, _ := s.Create(ctx, server.Document{Title: "doc"})
	_ = s.SaveRevision(ctx, makeRevision(doc.ID))
	_ = s.SaveRevision(ctx, makeRevision(doc.ID))

	if err := s.DeleteRevisions(ctx, doc.ID); err != nil {
		t.Fatalf("DeleteRevisions: %v", err)
	}

	revs, err := s.ListRevisions(ctx, doc.ID)
	if err != nil {
		t.Fatalf("ListRevisions after delete: %v", err)
	}
	if len(revs) != 0 {
		t.Errorf("want 0 revisions after delete, got %d", len(revs))
	}
	// Sequence reset: next save should start at 1.
	_ = s.SaveRevision(ctx, makeRevision(doc.ID))
	revs, _ = s.ListRevisions(ctx, doc.ID)
	if revs[0].Num != 1 {
		t.Errorf("want Num=1 after reset, got %d", revs[0].Num)
	}
}

func TestRevisionIsolatedByDoc(t *testing.T) {
	ctx := context.Background()
	s := newTestBolt(t)

	doc1, _ := s.Create(ctx, server.Document{Title: "doc1"})
	doc2, _ := s.Create(ctx, server.Document{Title: "doc2"})
	_ = s.SaveRevision(ctx, makeRevision(doc1.ID))
	_ = s.SaveRevision(ctx, makeRevision(doc1.ID))
	_ = s.SaveRevision(ctx, makeRevision(doc2.ID))

	revs1, _ := s.ListRevisions(ctx, doc1.ID)
	revs2, _ := s.ListRevisions(ctx, doc2.ID)
	if len(revs1) != 2 {
		t.Errorf("doc1: want 2, got %d", len(revs1))
	}
	if len(revs2) != 1 {
		t.Errorf("doc2: want 1, got %d", len(revs2))
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
