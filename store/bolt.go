package store

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	bolt "go.etcd.io/bbolt"

	"github.com/pasteai/pasteai/server"
)

var (
	bucketDocs      = []byte("documents")
	bucketByTime    = []byte("documents_by_time")
	bucketRevisions = []byte("revisions")
	bucketRevSeq    = []byte("revision_seq")
)

const maxRevisions = 50

var _ server.Store         = (*BoltStore)(nil) // compile-time interface check
var _ server.RevisionStore = (*BoltStore)(nil)

// BoltStore implements Store using bbolt for document metadata.
type BoltStore struct {
	db *bolt.DB
}

// DirFromDBPath derives the content files directory from the DB path.
// ~/.pasteai/documents.db  →  ~/.pasteai/documents/
func DirFromDBPath(path string) string {
	return filepath.Join(filepath.Dir(path), "documents")
}

// NewBolt opens or creates the bbolt database at path and ensures required buckets exist.
func NewBolt(path string) (*BoltStore, error) {
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open bbolt: %w", err)
	}
	err = db.Update(func(tx *bolt.Tx) error {
		for _, b := range [][]byte{bucketDocs, bucketByTime, bucketRevisions, bucketRevSeq} {
			if _, err := tx.CreateBucketIfNotExists(b); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create buckets: %w", err)
	}
	return &BoltStore{db: db}, nil
}

func (s *BoltStore) Close() error {
	return s.db.Close()
}

func (s *BoltStore) Create(_ context.Context, doc server.Document) (*server.Document, error) {
	doc.ID = uuid.New().String()
	if doc.Visibility == "" {
		doc.Visibility = server.VisibilityPublic
	}
	doc.CreatedAt = time.Now().UTC()
	doc.Content = "" // content is managed by ContentBackend

	data, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}

	timeKey := makeTimeKey(doc.CreatedAt, doc.ID)

	if err := s.db.Update(func(tx *bolt.Tx) error {
		if err := tx.Bucket(bucketDocs).Put([]byte(doc.ID), data); err != nil {
			return err
		}
		return tx.Bucket(bucketByTime).Put(timeKey, []byte(doc.ID))
	}); err != nil {
		return nil, err
	}
	return &doc, nil
}

func (s *BoltStore) List(_ context.Context, opts server.ListOptions) (*server.ListResult, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	var docs []server.Document

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
			// Seek lands on start or the next key after it; we want the key before start.
			_, _ = c.Seek(start)
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
			var doc server.Document
			if err := json.Unmarshal(data, &doc); err != nil {
				log.Printf("bolt: skipping corrupt document %q: %v", id, err)
				continue
			}
			visible := false
			if opts.OwnerID != "" {
				visible = doc.OwnerID == opts.OwnerID
			} else {
				visible = doc.Visibility == server.VisibilityPublic
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

	result := &server.ListResult{Documents: docs}
	// If we filled the page exactly, there may be more — provide a cursor.
	if len(docs) == limit {
		last := docs[len(docs)-1]
		result.NextToken = hex.EncodeToString(makeTimeKey(last.CreatedAt, last.ID))
	}
	return result, nil
}

func (s *BoltStore) Get(_ context.Context, id string) (*server.Document, error) {
	var doc server.Document
	err := s.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(bucketDocs).Get([]byte(id))
		if data == nil {
			return server.ErrNotFound
		}
		return json.Unmarshal(data, &doc)
	})
	if err != nil {
		return nil, err
	}
	// Content is managed by ContentBackend; return doc with empty Content.
	doc.Content = ""
	return &doc, nil
}

func (s *BoltStore) Update(_ context.Context, id, title string) (*server.Document, error) {
	return s.updateDoc(id, func(doc *server.Document) {
		if title != "" {
			doc.Title = title
		}
	})
}

func (s *BoltStore) UpdateVisibility(_ context.Context, id string, vis server.Visibility) (*server.Document, error) {
	return s.updateDoc(id, func(doc *server.Document) {
		doc.Visibility = vis
	})
}

func (s *BoltStore) updateDoc(id string, mutate func(*server.Document)) (*server.Document, error) {
	var doc server.Document
	if err := s.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(bucketDocs).Get([]byte(id))
		if data == nil {
			return server.ErrNotFound
		}
		return json.Unmarshal(data, &doc)
	}); err != nil {
		return nil, err
	}

	mutate(&doc)

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

	doc.Content = ""
	return &doc, nil
}

func (s *BoltStore) Delete(_ context.Context, id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		docBucket := tx.Bucket(bucketDocs)
		data := docBucket.Get([]byte(id))
		if data == nil {
			return server.ErrNotFound
		}
		var doc server.Document
		if err := json.Unmarshal(data, &doc); err != nil {
			return err
		}
		if err := docBucket.Delete([]byte(id)); err != nil {
			return err
		}
		timeKey := makeTimeKey(doc.CreatedAt, doc.ID)
		return tx.Bucket(bucketByTime).Delete(timeKey)
	})
}

func (s *BoltStore) Search(_ context.Context, opts server.SearchOptions) ([]server.Document, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	q := strings.ToLower(opts.Query)
	var docs []server.Document

	err := s.db.View(func(tx *bolt.Tx) error {
		timeBucket := tx.Bucket(bucketByTime)
		docBucket := tx.Bucket(bucketDocs)
		c := timeBucket.Cursor()

		for k, v := c.Last(); k != nil && len(docs) < limit; k, v = c.Prev() {
			id := string(v)
			data := docBucket.Get([]byte(id))
			if data == nil {
				continue
			}
			var doc server.Document
			if err := json.Unmarshal(data, &doc); err != nil {
				log.Printf("bolt: skipping corrupt document %q: %v", id, err)
				continue
			}
			visible := false
			if opts.OwnerID != "" {
				visible = doc.OwnerID == opts.OwnerID
			} else {
				visible = doc.Visibility == server.VisibilityPublic
			}
			if visible && strings.Contains(strings.ToLower(doc.Title), q) {
				docs = append(docs, doc)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return docs, nil
}

// makeTimeKey builds an 8-byte big-endian nanosecond timestamp followed by the doc ID.
// Big-endian ensures lexicographic order matches chronological order.
func makeTimeKey(t time.Time, id string) []byte {
	key := make([]byte, 8+len(id))
	binary.BigEndian.PutUint64(key[:8], uint64(t.UnixNano()))
	copy(key[8:], id)
	return key
}

// revKey builds the bucket key for a revision: "{docID}/{num:06d}".
func revKey(docID string, num int) []byte {
	return []byte(fmt.Sprintf("%s/%06d", docID, num))
}

// revPrefix returns the key prefix for all revisions of a document.
func revPrefix(docID string) []byte {
	return []byte(docID + "/")
}

func (s *BoltStore) SaveRevision(_ context.Context, rev server.Revision) (*server.Revision, error) {
	err := s.db.Update(func(tx *bolt.Tx) error {
		seq := tx.Bucket(bucketRevSeq)
		revs := tx.Bucket(bucketRevisions)

		// Increment sequence for this document.
		var num uint64
		if raw := seq.Get([]byte(rev.DocID)); raw != nil {
			num = binary.BigEndian.Uint64(raw)
		}
		num++
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], num)
		if err := seq.Put([]byte(rev.DocID), buf[:]); err != nil {
			return err
		}

		rev.Num = int(num)
		data, err := json.Marshal(rev)
		if err != nil {
			return err
		}
		if err := revs.Put(revKey(rev.DocID, rev.Num), data); err != nil {
			return err
		}

		// Prune oldest revisions if over the cap.
		prefix := revPrefix(rev.DocID)
		c := revs.Cursor()
		var keys [][]byte
		for k, _ := c.Seek(prefix); k != nil && strings.HasPrefix(string(k), string(prefix)); k, _ = c.Next() {
			keys = append(keys, append([]byte{}, k...))
		}
		for len(keys) > maxRevisions {
			if err := revs.Delete(keys[0]); err != nil {
				return err
			}
			keys = keys[1:]
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &rev, nil
}

func (s *BoltStore) ListRevisions(_ context.Context, docID string) ([]server.Revision, error) {
	var revs []server.Revision
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketRevisions)
		prefix := revPrefix(docID)
		c := b.Cursor()
		// Collect all matching keys then reverse for newest-first.
		var keys [][]byte
		for k, _ := c.Seek(prefix); k != nil && strings.HasPrefix(string(k), string(prefix)); k, _ = c.Next() {
			keys = append(keys, append([]byte{}, k...))
		}
		for i := len(keys) - 1; i >= 0; i-- {
			data := b.Get(keys[i])
			if data == nil {
				continue
			}
			var rev server.Revision
			if err := json.Unmarshal(data, &rev); err != nil {
				return err
			}
			revs = append(revs, rev)
		}
		return nil
	})
	return revs, err
}

func (s *BoltStore) GetRevision(_ context.Context, docID string, num int) (*server.Revision, error) {
	var rev server.Revision
	err := s.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(bucketRevisions).Get(revKey(docID, num))
		if data == nil {
			return server.ErrNotFound
		}
		return json.Unmarshal(data, &rev)
	})
	if err != nil {
		return nil, err
	}
	return &rev, nil
}

func (s *BoltStore) DeleteRevisions(_ context.Context, docID string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketRevisions)
		prefix := revPrefix(docID)
		c := b.Cursor()
		var keys [][]byte
		for k, _ := c.Seek(prefix); k != nil && strings.HasPrefix(string(k), string(prefix)); k, _ = c.Next() {
			keys = append(keys, append([]byte{}, k...))
		}
		for _, k := range keys {
			if err := b.Delete(k); err != nil {
				return err
			}
		}
		return tx.Bucket(bucketRevSeq).Delete([]byte(docID))
	})
}
