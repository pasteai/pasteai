package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func postDoc(t *testing.T, addr, title, content string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"title": title, "content": content})
	resp, err := http.Post("http://"+addr+"/api/documents", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", resp.StatusCode)
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result.ID
}

func TestServerCreateWritesFile(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	addr, tmpDir := startServer(t)

	id := postDoc(t, addr, "Hello", "# Hello\n\nWorld")

	filePath := filepath.Join(tmpDir, "documents", id+".md")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("content file not at %s: %v", filePath, err)
	}
	if string(data) != "# Hello\n\nWorld" {
		t.Errorf("file content = %q, want %q", string(data), "# Hello\n\nWorld")
	}
}

func TestServerGetReadsFile(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	addr, _ := startServer(t)

	id := postDoc(t, addr, "Title", "some content")

	resp, err := http.Get(fmt.Sprintf("http://%s/api/documents/%s", addr, id))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", resp.StatusCode)
	}
	var result struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Content != "some content" {
		t.Errorf("content = %q, want %q", result.Content, "some content")
	}
}

func TestServerDeleteRemovesFile(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	addr, tmpDir := startServer(t)

	id := postDoc(t, addr, "To delete", "bye")
	filePath := filepath.Join(tmpDir, "documents", id+".md")

	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("file should exist before delete: %v", err)
	}

	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("http://%s/api/documents/%s", addr, id), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", resp.StatusCode)
	}

	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Errorf("expected file gone after delete, got err: %v", err)
	}
}

func TestServerFileOfflineReadable(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	addr, tmpDir := startServer(t)

	content := "# Offline\n\nReadable without the server."
	id := postDoc(t, addr, "Offline", content)
	filePath := filepath.Join(tmpDir, "documents", id+".md")

	got, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("file not readable: %v", err)
	}
	if string(got) != content {
		t.Errorf("file content = %q, want %q", string(got), content)
	}
}

func TestServerConcurrentCreates(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	addr, tmpDir := startServer(t)

	const n = 10
	type result struct {
		id      string
		content string
	}
	results := make([]result, n)
	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			c := fmt.Sprintf("content-%d", i)
			id := postDoc(t, addr, fmt.Sprintf("Doc %d", i), c)
			results[i] = result{id: id, content: c}
		}()
	}
	wg.Wait()

	seen := map[string]bool{}
	for i, r := range results {
		if r.id == "" {
			t.Errorf("results[%d]: empty ID", i)
			continue
		}
		if seen[r.id] {
			t.Errorf("duplicate ID: %s", r.id)
		}
		seen[r.id] = true

		filePath := filepath.Join(tmpDir, "documents", r.id+".md")
		data, err := os.ReadFile(filePath)
		if err != nil {
			t.Errorf("file for results[%d] not found: %v", i, err)
			continue
		}
		if string(data) != r.content {
			t.Errorf("file %s = %q, want %q", r.id, string(data), r.content)
		}
	}
}
