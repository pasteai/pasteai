package integration

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type mcpMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  any             `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type toolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

type mcpProc struct {
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	cmd     *exec.Cmd
}

func startMCP(t *testing.T, serverURL string) *mcpProc {
	t.Helper()
	cmd := exec.Command(testBinaryPath, "mcp")
	cmd.Env = append(os.Environ(), "PASTEAI_URL="+serverURL)

	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		in.Close()
		cmd.Process.Kill()
		cmd.Wait()
	})

	return &mcpProc{stdin: in, scanner: bufio.NewScanner(out), cmd: cmd}
}

func (p *mcpProc) send(t *testing.T, msg any) {
	t.Helper()
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.stdin.Write(append(data, '\n')); err != nil {
		t.Fatal(err)
	}
}

func (p *mcpProc) recv(t *testing.T) mcpMsg {
	t.Helper()
	if !p.scanner.Scan() {
		t.Fatal("MCP stdout closed unexpectedly")
	}
	var msg mcpMsg
	if err := json.Unmarshal(p.scanner.Bytes(), &msg); err != nil {
		t.Fatalf("parse MCP response %q: %v", p.scanner.Text(), err)
	}
	return msg
}

func (p *mcpProc) initialize(t *testing.T) mcpMsg {
	t.Helper()
	id := 1
	p.send(t, mcpMsg{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test", "version": "1"},
		},
	})
	resp := p.recv(t)
	// Send initialized notification — no response expected
	p.send(t, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	return resp
}

func (p *mcpProc) callTool(t *testing.T, id int, name string, args map[string]any) toolResult {
	t.Helper()
	mid := id
	p.send(t, mcpMsg{
		JSONRPC: "2.0",
		ID:      &mid,
		Method:  "tools/call",
		Params:  map[string]any{"name": name, "arguments": args},
	})
	resp := p.recv(t)
	if resp.Error != nil {
		t.Fatalf("tools/call %s: JSON-RPC error %d: %s", name, resp.Error.Code, resp.Error.Message)
	}
	var tr toolResult
	if err := json.Unmarshal(resp.Result, &tr); err != nil {
		t.Fatalf("parse tool result: %v", err)
	}
	return tr
}

func TestMCPInitialize(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	addr, _ := startServer(t)
	p := startMCP(t, "http://"+addr)

	initResp := p.initialize(t)
	if initResp.Error != nil {
		t.Fatalf("initialize error: %v", initResp.Error)
	}

	id := 2
	p.send(t, mcpMsg{JSONRPC: "2.0", ID: &id, Method: "tools/list"})
	listResp := p.recv(t)
	if listResp.Error != nil {
		t.Fatalf("tools/list error: %v", listResp.Error)
	}

	raw := string(listResp.Result)
	if !strings.Contains(raw, "publish_document") {
		t.Errorf("publish_document not in tools/list: %s", raw)
	}
	if !strings.Contains(raw, "list_documents") {
		t.Errorf("list_documents not in tools/list: %s", raw)
	}
}

func TestMCPPublishCreatesFile(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	addr, tmpDir := startServer(t)
	p := startMCP(t, "http://"+addr)
	p.initialize(t)

	tr := p.callTool(t, 2, "publish_document", map[string]any{
		"title":   "MCP Test",
		"content": "# MCP Test\n\nPublished via MCP.",
	})
	if tr.IsError {
		t.Fatalf("publish failed: %s", tr.Content[0].Text)
	}

	text := tr.Content[0].Text
	docID := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "ID: ") {
			docID = strings.TrimPrefix(line, "ID: ")
		}
	}
	if docID == "" {
		t.Fatalf("could not parse ID from: %q", text)
	}

	filePath := filepath.Join(tmpDir, "documents", docID+".md")
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("expected content file at %s: %v", filePath, err)
	}
}

func TestMCPListReturnsDoc(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	addr, _ := startServer(t)
	p := startMCP(t, "http://"+addr)
	p.initialize(t)

	p.callTool(t, 2, "publish_document", map[string]any{
		"title":   "Listed Doc",
		"content": "hello",
	})

	tr := p.callTool(t, 3, "list_documents", map[string]any{})
	if tr.IsError {
		t.Fatalf("list failed: %s", tr.Content[0].Text)
	}
	if !strings.Contains(tr.Content[0].Text, "Listed Doc") {
		t.Errorf("'Listed Doc' not in list response: %s", tr.Content[0].Text)
	}
}

func TestMCPPublishMissingParams(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	addr, _ := startServer(t)
	p := startMCP(t, "http://"+addr)
	p.initialize(t)

	tr := p.callTool(t, 2, "publish_document", map[string]any{})
	if !tr.IsError {
		t.Error("expected isError=true for missing title/content")
	}

	// MCP process must still respond after an error
	tr2 := p.callTool(t, 3, "list_documents", map[string]any{})
	if tr2.IsError {
		t.Errorf("process appears dead after error: %s", tr2.Content[0].Text)
	}
}

func TestMCPGetDocument(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	addr, _ := startServer(t)
	p := startMCP(t, "http://"+addr)
	p.initialize(t)

	p.callTool(t, 2, "publish_document", map[string]any{
		"title":   "Get Test",
		"content": "# Hello\n\nWorld",
	})

	// list to get the ID
	listTR := p.callTool(t, 3, "list_documents", map[string]any{})
	text := listTR.Content[0].Text
	docID := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "ID: ") {
			start := strings.Index(line, "ID: ") + 4
			end := strings.Index(line[start:], ",")
			if end == -1 {
				end = strings.Index(line[start:], ")")
			}
			if end != -1 {
				docID = line[start : start+end]
			}
		}
	}
	if docID == "" {
		t.Fatal("could not parse ID from list output")
	}

	tr := p.callTool(t, 4, "get_document", map[string]any{"id": docID})
	if tr.IsError {
		t.Fatalf("get_document failed: %s", tr.Content[0].Text)
	}
	got := tr.Content[0].Text
	if !strings.Contains(got, "Get Test") {
		t.Errorf("title not in get result: %s", got)
	}
	if !strings.Contains(got, "# Hello") {
		t.Errorf("content not in get result: %s", got)
	}
}

func TestMCPUpdateDocument(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	addr, _ := startServer(t)
	p := startMCP(t, "http://"+addr)
	p.initialize(t)

	tr := p.callTool(t, 2, "publish_document", map[string]any{
		"title":   "Original",
		"content": "original content",
	})
	if tr.IsError {
		t.Fatalf("publish failed: %s", tr.Content[0].Text)
	}
	docID := ""
	for _, line := range strings.Split(tr.Content[0].Text, "\n") {
		if strings.HasPrefix(line, "ID: ") {
			docID = strings.TrimPrefix(line, "ID: ")
		}
	}
	if docID == "" {
		t.Fatal("could not parse ID from publish output")
	}

	tr2 := p.callTool(t, 3, "update_document", map[string]any{
		"id":      docID,
		"title":   "Updated Title",
		"content": "updated content",
	})
	if tr2.IsError {
		t.Fatalf("update_document failed: %s", tr2.Content[0].Text)
	}
	if !strings.Contains(tr2.Content[0].Text, "updated") {
		t.Errorf("expected 'updated' in result: %s", tr2.Content[0].Text)
	}

	// Verify via get_document
	tr3 := p.callTool(t, 4, "get_document", map[string]any{"id": docID})
	if tr3.IsError {
		t.Fatalf("get after update failed: %s", tr3.Content[0].Text)
	}
	got := tr3.Content[0].Text
	if !strings.Contains(got, "Updated Title") {
		t.Errorf("updated title not found: %s", got)
	}
	if !strings.Contains(got, "updated content") {
		t.Errorf("updated content not found: %s", got)
	}
}

func TestMCPServerUnreachable(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	serveCmd, addr, _ := startServeCmd(t)
	p := startMCP(t, "http://"+addr)
	p.initialize(t)

	// First publish succeeds
	tr := p.callTool(t, 2, "publish_document", map[string]any{
		"title":   "Before kill",
		"content": "hello",
	})
	if tr.IsError {
		t.Fatalf("first publish failed: %s", tr.Content[0].Text)
	}

	// Kill the server
	serveCmd.Process.Kill()
	serveCmd.Wait()

	// Second publish should return isError: true (server unreachable)
	tr2 := p.callTool(t, 3, "publish_document", map[string]any{
		"title":   "After kill",
		"content": "hello",
	})
	if !tr2.IsError {
		t.Error("expected isError=true after server kill")
	}

	// MCP process must still be alive
	tr3 := p.callTool(t, 4, "list_documents", map[string]any{})
	_ = tr3 // also an error (server gone) but MCP process responded
}
