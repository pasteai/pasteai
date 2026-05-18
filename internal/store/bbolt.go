package store

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	bolt "go.etcd.io/bbolt"
)

var (
	bucketDocs   = []byte("documents")
	bucketByTime = []byte("documents_by_time")
)

var _ Store = (*BoltStore)(nil) // compile-time interface check

type BoltStore struct {
	db       *bolt.DB
	filesDir string
}

// dirFromDBPath derives the content files directory from the DB path.
// ~/.pasteai/documents.db  →  ~/.pasteai/documents/
func dirFromDBPath(path string) string {
	return filepath.Join(filepath.Dir(path), "documents")
}

func NewBolt(path string) (*BoltStore, error) {
	filesDir := dirFromDBPath(path)
	if err := os.MkdirAll(filesDir, 0700); err != nil {
		return nil, fmt.Errorf("create documents dir: %w", err)
	}

	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open bbolt: %w", err)
	}
	err = db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(bucketDocs); err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists(bucketByTime)
		return err
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create buckets: %w", err)
	}
	return &BoltStore{db: db, filesDir: filesDir}, nil
}

func (s *BoltStore) Close() error {
	return s.db.Close()
}

func (s *BoltStore) contentPath(id string) string {
	return filepath.Join(s.filesDir, id+".md")
}

func (s *BoltStore) Create(_ context.Context, doc Document) (*Document, error) {
	doc.ID = uuid.New().String()
	if doc.Visibility == "" {
		doc.Visibility = VisibilityPublic
	}
	doc.CreatedAt = time.Now().UTC()

	// Write content to disk first — clean failure if this fails (nothing in bbolt yet).
	if err := os.WriteFile(s.contentPath(doc.ID), []byte(doc.Content), 0600); err != nil {
		return nil, fmt.Errorf("write content file: %w", err)
	}

	// Marshal metadata only — content lives on disk.
	meta := doc
	meta.Content = ""
	data, err := json.Marshal(meta)
	if err != nil {
		os.Remove(s.contentPath(doc.ID))
		return nil, err
	}

	timeKey := makeTimeKey(doc.CreatedAt, doc.ID)

	if err := s.db.Update(func(tx *bolt.Tx) error {
		if err := tx.Bucket(bucketDocs).Put([]byte(doc.ID), data); err != nil {
			return err
		}
		return tx.Bucket(bucketByTime).Put(timeKey, []byte(doc.ID))
	}); err != nil {
		// bbolt write failed — content file is orphaned on disk (not indexed, benign).
		log.Printf("pasteai: bbolt write failed for %s, content file orphaned: %v", doc.ID, err)
		return nil, err
	}
	return &doc, nil
}

func (s *BoltStore) List(_ context.Context, opts ListOptions) (*ListResult, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	var docs []Document

	err := s.db.View(func(tx *bolt.Tx) error {
		timeBucket := tx.Bucket(bucketByTime)
		docBucket := tx.Bucket(bucketDocs)

		c := timeBucket.Cursor()

		// NextToken is a hex-encoded time key; decode it to a binary seek position.
		var start []byte
		if opts.NextToken != "" {
			if decoded, err := hex.DecodeString(opts.NextToken); err == nil {
				start = decoded
			}
			// Invalid cursor silently falls back to start of list (first page).
		}

		var k, v []byte
		if start != nil {
			k, v = c.Seek(start)
			// Seek lands on start or the next key after it; we want the key before start.
			k, v = c.Prev()
		} else {
			k, v = c.Last()
		}

		for ; k != nil && len(docs) < limit; k, v = c.Prev() {
			id := string(v)
			data := docBucket.Get([]byte(id))
			if data == nil {
				continue
			}
			var doc Document
			if err := json.Unmarshal(data, &doc); err != nil {
				continue
			}
			visible := false
			if opts.OwnerID != "" {
				visible = doc.OwnerID == opts.OwnerID
			} else {
				visible = doc.Visibility == VisibilityPublic
			}
			if visible {
				docs = append(docs, doc)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	result := &ListResult{Documents: docs}
	// If we filled the page exactly, there may be more — provide a cursor.
	if len(docs) == limit {
		last := docs[len(docs)-1]
		result.NextToken = hex.EncodeToString(makeTimeKey(last.CreatedAt, last.ID))
	}
	return result, nil
}

func (s *BoltStore) Get(_ context.Context, id string) (*Document, error) {
	var doc Document
	err := s.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(bucketDocs).Get([]byte(id))
		if data == nil {
			return ErrNotFound
		}
		return json.Unmarshal(data, &doc)
	})
	if err != nil {
		return nil, err
	}

	// Read content from disk.
	content, err := os.ReadFile(s.contentPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrContentMissing, id)
		}
		return nil, fmt.Errorf("read content file: %w", err)
	}
	doc.Content = string(content)
	return &doc, nil
}

func (s *BoltStore) Update(_ context.Context, id, title, content string) (*Document, error) {
	var doc Document
	if err := s.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(bucketDocs).Get([]byte(id))
		if data == nil {
			return ErrNotFound
		}
		return json.Unmarshal(data, &doc)
	}); err != nil {
		return nil, err
	}

	if title != "" {
		doc.Title = title
	}
	if content != "" {
		if err := os.WriteFile(s.contentPath(id), []byte(content), 0600); err != nil {
			return nil, fmt.Errorf("write content file: %w", err)
		}
		doc.Content = content
	}

	meta := doc
	meta.Content = ""
	data, err := json.Marshal(meta)
	if err != nil {
		return nil, err
	}
	if err := s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketDocs).Put([]byte(id), data)
	}); err != nil {
		return nil, err
	}

	if content == "" {
		raw, err := os.ReadFile(s.contentPath(id))
		if err == nil {
			doc.Content = string(raw)
		}
	}
	return &doc, nil
}

func (s *BoltStore) Delete(_ context.Context, id string) error {
	err := s.db.Update(func(tx *bolt.Tx) error {
		docBucket := tx.Bucket(bucketDocs)
		data := docBucket.Get([]byte(id))
		if data == nil {
			return ErrNotFound
		}
		var doc Document
		if err := json.Unmarshal(data, &doc); err != nil {
			return err
		}
		if err := docBucket.Delete([]byte(id)); err != nil {
			return err
		}
		timeKey := makeTimeKey(doc.CreatedAt, doc.ID)
		return tx.Bucket(bucketByTime).Delete(timeKey)
	})
	if err != nil {
		return err
	}

	// Remove content file — non-fatal if already missing.
	if err := os.Remove(s.contentPath(id)); err != nil && !os.IsNotExist(err) {
		log.Printf("pasteai: delete content file %s: %v", id, err)
	}
	return nil
}

// makeTimeKey builds an 8-byte big-endian nanosecond timestamp followed by the doc ID.
// Big-endian ensures lexicographic order matches chronological order.
func makeTimeKey(t time.Time, id string) []byte {
	key := make([]byte, 8+len(id))
	binary.BigEndian.PutUint64(key[:8], uint64(t.UnixNano()))
	copy(key[8:], id)
	return key
}
