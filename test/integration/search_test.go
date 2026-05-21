package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestSearchReturnsMatches(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	addr, _ := startServer(t)

	postDoc(t, addr, "Auth flow guide", "authentication content")
	postDoc(t, addr, "Deployment notes", "deploy content")

	resp, err := http.Get(fmt.Sprintf("http://%s/api/search?q=auth", addr))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search: expected 200, got %d", resp.StatusCode)
	}
	var result struct {
		Documents []struct {
			Title string `json:"title"`
		} `json:"documents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result.Documents) != 1 {
		t.Fatalf("got %d documents, want 1", len(result.Documents))
	}
	if result.Documents[0].Title != "Auth flow guide" {
		t.Errorf("got title %q, want Auth flow guide", result.Documents[0].Title)
	}
}

func TestSearchCaseInsensitive(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	addr, _ := startServer(t)

	postDoc(t, addr, "Auth flow guide", "content")

	resp, err := http.Get(fmt.Sprintf("http://%s/api/search?q=AUTH", addr))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search: expected 200, got %d", resp.StatusCode)
	}
	var result struct {
		Documents []struct {
			Title string `json:"title"`
		} `json:"documents"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Documents) != 1 {
		t.Errorf("case-insensitive search: got %d documents, want 1", len(result.Documents))
	}
}

func TestSearchNoResults(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	addr, _ := startServer(t)

	postDoc(t, addr, "Hello world", "content")

	resp, err := http.Get(fmt.Sprintf("http://%s/api/search?q=nomatch", addr))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search: expected 200, got %d", resp.StatusCode)
	}
	var result struct {
		Documents []struct {
			Title string `json:"title"`
		} `json:"documents"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Documents) != 0 {
		t.Errorf("expected empty results, got %d", len(result.Documents))
	}
}

func TestSearchEmptyQueryReturns400(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	addr, _ := startServer(t)

	resp, err := http.Get(fmt.Sprintf("http://%s/api/search", addr))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("empty query: expected 400, got %d", resp.StatusCode)
	}
}

func TestSearchHomePage(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	addr, _ := startServer(t)

	postDoc(t, addr, "Auth flow guide", "auth content")
	postDoc(t, addr, "Deployment notes", "deploy content")

	resp, err := http.Get(fmt.Sprintf("http://%s/?q=auth", addr))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("home search: expected 200, got %d", resp.StatusCode)
	}
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	html := sb.String()

	if !strings.Contains(html, "Auth flow guide") {
		t.Error("matching doc not in home search results")
	}
	if !strings.Contains(html, "Search results for") {
		t.Error("search results heading not found")
	}
}
