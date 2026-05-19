package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pasteai/pasteai/server"
)

var _ server.ContentBackend = (*DiskContent)(nil) // compile-time interface check

type DiskContent struct {
	dir string
}

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
