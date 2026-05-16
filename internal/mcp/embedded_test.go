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

	"github.com/pasteai/pasteai/internal/api"
	"github.com/pasteai/pasteai/internal/store"
)

func TestEmbeddedServerStartup(t *testing.T) {
	// Stand up an api.Server on a random port, the same way startEmbedded would.
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.NewBolt(dbPath)
	if err != nil {
		t.Fatalf("NewBolt: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	httpSrv := api.NewServer(s, api.Config{
		Addr:   ":0",
		Logger: log.New(io.Discard, "", 0),
	})
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	go httpSrv.Serve(ln)
	t.Cleanup(func() { httpSrv.Close() })

	baseURL := fmt.Sprintf("http://localhost:%d", port)

	// isResponding should return true once the server is up
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

	// Publish a document via the HTTP API (same path the MCP handlePublish takes)
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
