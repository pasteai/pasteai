package mcp

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pasteai/pasteai/server"
	"github.com/pasteai/pasteai/store"
)

func TestEmbeddedServerStartup(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.NewBolt(dbPath)
	if err != nil {
		t.Fatalf("NewBolt: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	c, err := store.NewDiskContent(store.DirFromDBPath(dbPath))
	if err != nil {
		t.Fatalf("NewDiskContent: %v", err)
	}

	handler := server.NewServer(s, c, server.Options{
		Logger: log.New(io.Discard, "", 0),
	})
	httpSrv := &http.Server{Handler: handler}
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	go httpSrv.Serve(ln)
	t.Cleanup(func() { httpSrv.Close() })

	baseURL := fmt.Sprintf("http://localhost:%d", port)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if isResponding(baseURL) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !isResponding(baseURL) {
		t.Fatal("embedded server did not become ready")
	}

	body := strings.NewReader(`{"title":"Test","content":"# Hello\n\nWorld"}`)
	resp, err := http.Post(baseURL+"/api/documents", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/documents: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
}
