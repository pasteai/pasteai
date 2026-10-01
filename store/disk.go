package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/pasteai/pasteai/server"
)

var _ server.ContentBackend         = (*DiskContent)(nil) // compile-time interface check
var _ server.RevisionContentBackend = (*DiskContent)(nil)

// DiskContent implements ContentBackend by storing document content as files on disk.
type DiskContent struct {
	dir string
}

// NewDiskContent creates a DiskContent that stores files under dir, creating it if needed.
func NewDiskContent(dir string) (*DiskContent, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create content dir: %w", err)
	}
	return &DiskContent{dir: dir}, nil
}

func (d *DiskContent) path(id string) string {
	return filepath.Join(d.dir, id+".md")
}

func (d *DiskContent) Put(_ context.Context, id string, content []byte) error {
	if err := os.WriteFile(d.path(id), content, 0600); err != nil {
		return fmt.Errorf("write content file: %w", err)
	}
	return nil
}

func (d *DiskContent) Get(_ context.Context, id string) ([]byte, error) {
	data, err := os.ReadFile(d.path(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", server.ErrNotFound, id)
		}
		return nil, fmt.Errorf("read content file: %w", err)
	}
	return data, nil
}

func (d *DiskContent) Delete(_ context.Context, id string) error {
	err := os.Remove(d.path(id))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete content file: %w", err)
	}
	return nil
}

func (d *DiskContent) revPath(docID string, num int) string {
	return filepath.Join(d.dir, "revisions", docID, fmt.Sprintf("%06d.md", num))
}

// PutRevision writes a revision content snapshot to disk.
func (d *DiskContent) PutRevision(_ context.Context, docID string, num int, content []byte) error {
	p := d.revPath(docID, num)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return fmt.Errorf("create revision dir: %w", err)
	}
	if err := os.WriteFile(p, content, 0600); err != nil {
		return fmt.Errorf("write revision file: %w", err)
	}
	return nil
}

// GetRevision reads a revision content snapshot from disk.
func (d *DiskContent) GetRevision(_ context.Context, docID string, num int) ([]byte, error) {
	data, err := os.ReadFile(d.revPath(docID, num))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: revision %d of %s", server.ErrNotFound, num, docID)
		}
		return nil, fmt.Errorf("read revision file: %w", err)
	}
	return data, nil
}

// DeleteRevisions removes all revision content files for a document.
func (d *DiskContent) DeleteRevisions(_ context.Context, docID string) error {
	dir := filepath.Join(d.dir, "revisions", docID)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("delete revision dir: %w", err)
	}
	return nil
}

func (d *DiskContent) commentDir(docID string) string {
	return filepath.Join(d.dir, "comments", docID)
}

func (d *DiskContent) commentPath(docID, commentID string) string {
	return filepath.Join(d.commentDir(docID), commentID+".json")
}

// AddComment writes a new comment as a JSON file, assigning a UUID and CreatedAt.
func (d *DiskContent) AddComment(_ context.Context, c server.Comment) (*server.Comment, error) {
	c.ID = uuid.New().String()
	c.CreatedAt = time.Now().UTC()
	if err := os.MkdirAll(d.commentDir(c.DocID), 0700); err != nil {
		return nil, fmt.Errorf("create comment dir: %w", err)
	}
	data, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("marshal comment: %w", err)
	}
	if err := os.WriteFile(d.commentPath(c.DocID, c.ID), data, 0600); err != nil {
		return nil, fmt.Errorf("write comment file: %w", err)
	}
	return &c, nil
}

// ListComments returns all comments for docID ordered by CreatedAt ascending.
func (d *DiskContent) ListComments(_ context.Context, docID string) ([]server.Comment, error) {
	dir := d.commentDir(docID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []server.Comment{}, nil
		}
		return nil, fmt.Errorf("read comment dir: %w", err)
	}
	comments := make([]server.Comment, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read comment file: %w", err)
		}
		var c server.Comment
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("unmarshal comment: %w", err)
		}
		comments = append(comments, c)
	}
	sort.Slice(comments, func(i, j int) bool {
		return comments[i].CreatedAt.Before(comments[j].CreatedAt)
	})
	return comments, nil
}

// GetComment returns the comment with the given ID, or ErrNotFound.
func (d *DiskContent) GetComment(_ context.Context, docID, commentID string) (*server.Comment, error) {
	data, err := os.ReadFile(d.commentPath(docID, commentID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: comment %s", server.ErrNotFound, commentID)
		}
		return nil, fmt.Errorf("read comment file: %w", err)
	}
	var c server.Comment
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("unmarshal comment: %w", err)
	}
	return &c, nil
}

// ResolveComment sets the Resolved field and returns the updated comment.
func (d *DiskContent) ResolveComment(_ context.Context, docID, commentID string, resolved bool) (*server.Comment, error) {
	p := d.commentPath(docID, commentID)
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: comment %s", server.ErrNotFound, commentID)
		}
		return nil, fmt.Errorf("read comment file: %w", err)
	}
	var c server.Comment
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("unmarshal comment: %w", err)
	}
	c.Resolved = resolved
	updated, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("marshal comment: %w", err)
	}
	if err := os.WriteFile(p, updated, 0600); err != nil {
		return nil, fmt.Errorf("write comment file: %w", err)
	}
	return &c, nil
}

// UpdateCommentBody replaces the body text of a comment and returns the updated comment.
// Returns ErrNotFound if the comment file does not exist.
func (d *DiskContent) UpdateCommentBody(_ context.Context, docID, commentID, body string) (*server.Comment, error) {
	p := d.commentPath(docID, commentID)
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: comment %s", server.ErrNotFound, commentID)
		}
		return nil, fmt.Errorf("read comment file: %w", err)
	}
	var c server.Comment
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("unmarshal comment: %w", err)
	}
	c.Body = body
	updated, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("marshal comment: %w", err)
	}
	if err := os.WriteFile(p, updated, 0600); err != nil {
		return nil, fmt.Errorf("write comment file: %w", err)
	}
	return &c, nil
}

// DeleteComment permanently removes a comment. Returns ErrNotFound if missing.
func (d *DiskContent) DeleteComment(_ context.Context, docID, commentID string) error {
	p := d.commentPath(docID, commentID)
	err := os.Remove(p)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: comment %s", server.ErrNotFound, commentID)
		}
		return fmt.Errorf("delete comment file: %w", err)
	}
	return nil
}
