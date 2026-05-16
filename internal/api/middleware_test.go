package api_test

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pasteai/pasteai/internal/api"
	"github.com/pasteai/pasteai/internal/store"
)

func newServerWithBaseURL(t *testing.T, baseURL string) (*httptest.Server, store.Store) {
	t.Helper()
	dir := t.TempDir()
	s, _ := store.NewBolt(filepath.Join(dir, "test.db"))
	t.Cleanup(func() { s.Close() })

	httpSrv := api.NewServer(s, api.Config{
		Addr:    ":0",
		BaseURL: baseURL,
		Logger:  log.New(io.Discard, "", 0),
	})
	ts := httptest.NewServer(httpSrv.Handler)
	t.Cleanup(ts.Close)
	return ts, s
}

func TestDocURLFromBaseURL(t *testing.T) {
	ts, _ := newServerWithBaseURL(t, "https://pasteai.io")

	body := `{"title":"Test","content":"body"}`
	resp, err := http.Post(ts.URL+"/api/documents", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var result map[string]any
	decodeJSON(t, resp.Body, &result)

	url, _ := result["url"].(string)
	if !strings.HasPrefix(url, "https://pasteai.io/d/") {
		t.Errorf("url = %q, want prefix https://pasteai.io/d/", url)
	}
}

func TestDocURLDerivedFromRequest(t *testing.T) {
	ts, _ := newServerWithBaseURL(t, "")

	body := `{"title":"Test","content":"body"}`
	resp, err := http.Post(ts.URL+"/api/documents", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var result map[string]any
	decodeJSON(t, resp.Body, &result)

	url, _ := result["url"].(string)
	if !strings.Contains(url, "/d/") {
		t.Errorf("url = %q, expected /d/ path", url)
	}
	if !strings.HasPrefix(url, "http://127.0.0.1") {
		t.Errorf("url = %q, expected http://127.0.0.1 prefix", url)
	}
}

func decodeJSON(t *testing.T, r io.Reader, v any) {
	t.Helper()
	if err := json.NewDecoder(r).Decode(v); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
}
