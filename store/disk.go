package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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
