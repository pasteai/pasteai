package setup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readJSONFile(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return result
}

func getPasteaiEntryUnit(t *testing.T, cfg map[string]any) map[string]any {
	t.Helper()
	servers, ok := cfg["mcpServers"].(map[string]any)
	if !ok {
		t.Fatal("mcpServers key missing or wrong type")
	}
	entry, ok := servers["pasteai"].(map[string]any)
	if !ok {
		t.Fatal("pasteai entry missing or wrong type")
	}
	return entry
}

// --- JSON merge tests ---

func TestMergeEmbedded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.json")

	action, err := mergeClaudeJSON(path, "/fake/pasteai", modeEmbedded, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if action != "Added" {
		t.Errorf("action = %s, want Added", action)
	}
	entry := getPasteaiEntryUnit(t, readJSONFile(t, path))
	if _, hasEnv := entry["env"]; hasEnv {
		t.Error("embedded mode should have no env block")
	}
	if entry["command"] != "/fake/pasteai" {
		t.Errorf("command = %v", entry["command"])
	}
}

func TestMergeLocal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.json")

	_, err := mergeClaudeJSON(path, "/fake/pasteai", modeLocal, "", "")
	if err != nil {
		t.Fatal(err)
	}
	entry := getPasteaiEntryUnit(t, readJSONFile(t, path))
	env, ok := entry["env"].(map[string]any)
	if !ok {
		t.Fatalf("local mode must have env block, got %v", entry)
	}
	if env["PASTEAI_URL"] != "http://localhost:8080" {
		t.Errorf("PASTEAI_URL = %v", env["PASTEAI_URL"])
	}
}

func TestMergeRemoteWithKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.json")

	_, err := mergeClaudeJSON(path, "/fake/pasteai", modeRemote, "https://example.com", "mykey")
	if err != nil {
		t.Fatal(err)
	}
	entry := getPasteaiEntryUnit(t, readJSONFile(t, path))
	env, ok := entry["env"].(map[string]any)
	if !ok {
		t.Fatalf("remote mode must have env block")
	}
	if env["PASTEAI_URL"] != "https://example.com" {
		t.Errorf("PASTEAI_URL = %v", env["PASTEAI_URL"])
	}
	if env["PASTEAI_API_KEY"] != "mykey" {
		t.Errorf("PASTEAI_API_KEY = %v", env["PASTEAI_API_KEY"])
	}
}

func TestMergeRemoteNoKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.json")

	_, err := mergeClaudeJSON(path, "/fake/pasteai", modeRemote, "https://example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	entry := getPasteaiEntryUnit(t, readJSONFile(t, path))
	env, ok := entry["env"].(map[string]any)
	if !ok {
		t.Fatalf("remote mode must have env block")
	}
	if _, hasKey := env["PASTEAI_API_KEY"]; hasKey {
		t.Error("PASTEAI_API_KEY should be absent when not provided")
	}
}

func TestMergeMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.json")

	action, err := mergeClaudeJSON(path, "/fake/pasteai", modeEmbedded, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if action != "Added" {
		t.Errorf("action = %s, want Added", action)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("file should be created")
	}
}

func TestMergeMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.json")

	if err := os.WriteFile(path, []byte("{ not valid json"), 0600); err != nil {
		t.Fatal(err)
	}

	action, err := mergeClaudeJSON(path, "/fake/pasteai", modeEmbedded, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if action != "Added" {
		t.Errorf("action = %s, want Added (malformed treated as empty)", action)
	}
	getPasteaiEntryUnit(t, readJSONFile(t, path))
}

func TestMergePreservesOtherServers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.json")

	existing := map[string]any{
		"mcpServers": map[string]any{
			"other-tool": map[string]any{
				"command": "/usr/bin/other",
				"args":    []any{"serve"},
			},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(path, append(data, '\n'), 0600)

	_, err := mergeClaudeJSON(path, "/fake/pasteai", modeEmbedded, "", "")
	if err != nil {
		t.Fatal(err)
	}

	result := readJSONFile(t, path)
	servers := result["mcpServers"].(map[string]any)
	if _, ok := servers["other-tool"]; !ok {
		t.Error("other-tool was removed")
	}
	if _, ok := servers["pasteai"]; !ok {
		t.Error("pasteai was not added")
	}
}

func TestMergeIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.json")

	if _, err := mergeClaudeJSON(path, "/fake/pasteai", modeEmbedded, "", ""); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)

	if _, err := mergeClaudeJSON(path, "/fake/pasteai", modeEmbedded, "", ""); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)

	if !bytes.Equal(first, second) {
		t.Errorf("idempotency failure:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestMergeModeSwitch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.json")

	existing := map[string]any{
		"mcpServers": map[string]any{
			"other-tool": map[string]any{"command": "/usr/bin/other", "args": []any{"serve"}},
			"pasteai":    map[string]any{"command": "/fake/pasteai", "args": []any{"mcp"}},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(path, append(data, '\n'), 0600)

	action, err := mergeClaudeJSON(path, "/fake/pasteai", modeRemote, "https://example.com", "key")
	if err != nil {
		t.Fatal(err)
	}
	if action != "Updated" {
		t.Errorf("action = %s, want Updated", action)
	}

	result := readJSONFile(t, path)
	servers := result["mcpServers"].(map[string]any)

	if _, ok := servers["other-tool"]; !ok {
		t.Error("other-tool was removed")
	}

	entry := servers["pasteai"].(map[string]any)
	env, ok := entry["env"].(map[string]any)
	if !ok {
		t.Fatal("remote entry must have env block")
	}
	if env["PASTEAI_URL"] != "https://example.com" {
		t.Errorf("PASTEAI_URL = %v", env["PASTEAI_URL"])
	}
}

func TestMergePreservesExtraEntryFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.json")

	existing := map[string]any{
		"mcpServers": map[string]any{
			"pasteai": map[string]any{
				"command":    "/old/pasteai",
				"args":       []any{"mcp"},
				"user_field": "keep-me",
			},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(path, append(data, '\n'), 0600)

	_, err := mergeClaudeJSON(path, "/new/pasteai", modeEmbedded, "", "")
	if err != nil {
		t.Fatal(err)
	}
	entry := getPasteaiEntryUnit(t, readJSONFile(t, path))
	if entry["command"] != "/new/pasteai" {
		t.Errorf("command not updated: %v", entry["command"])
	}
	if entry["user_field"] != "keep-me" {
		t.Errorf("user_field was removed: %v", entry)
	}
}

func TestMergePreservesUserEnvVars(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.json")

	existing := map[string]any{
		"mcpServers": map[string]any{
			"pasteai": map[string]any{
				"command": "/old/pasteai",
				"args":    []any{"mcp"},
				"env": map[string]any{
					"PASTEAI_URL":    "http://old:8080",
					"MY_CUSTOM_FLAG": "yes",
				},
			},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(path, append(data, '\n'), 0600)

	_, err := mergeClaudeJSON(path, "/new/pasteai", modeLocal, "", "")
	if err != nil {
		t.Fatal(err)
	}
	entry := getPasteaiEntryUnit(t, readJSONFile(t, path))
	env, ok := entry["env"].(map[string]any)
	if !ok {
		t.Fatal("env block missing")
	}
	if env["PASTEAI_URL"] != "http://localhost:8080" {
		t.Errorf("PASTEAI_URL not updated: %v", env["PASTEAI_URL"])
	}
	if env["MY_CUSTOM_FLAG"] != "yes" {
		t.Errorf("MY_CUSTOM_FLAG was removed: %v", env)
	}
}

func TestMergeSwitchToEmbeddedRemovesURLKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.json")

	existing := map[string]any{
		"mcpServers": map[string]any{
			"pasteai": map[string]any{
				"command": "/old/pasteai",
				"args":    []any{"mcp"},
				"env": map[string]any{
					"PASTEAI_URL":     "https://remote.example.com",
					"PASTEAI_API_KEY": "secret",
					"MY_CUSTOM_FLAG":  "yes",
				},
			},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(path, append(data, '\n'), 0600)

	_, err := mergeClaudeJSON(path, "/new/pasteai", modeEmbedded, "", "")
	if err != nil {
		t.Fatal(err)
	}
	entry := getPasteaiEntryUnit(t, readJSONFile(t, path))
	env, ok := entry["env"].(map[string]any)
	if !ok {
		t.Fatal("env block missing (expected because MY_CUSTOM_FLAG survived)")
	}
	if _, has := env["PASTEAI_URL"]; has {
		t.Error("PASTEAI_URL should be removed when switching to embedded")
	}
	if _, has := env["PASTEAI_API_KEY"]; has {
		t.Error("PASTEAI_API_KEY should be removed when switching to embedded")
	}
	if env["MY_CUSTOM_FLAG"] != "yes" {
		t.Errorf("MY_CUSTOM_FLAG was removed: %v", env)
	}
}

// --- Atomicity tests ---

func TestValidationBeforeWrite_RemoteNoURL(t *testing.T) {
	err := Run([]string{"-mode", "remote", "-binary", "/fake/pasteai"})
	if err == nil {
		t.Fatal("expected error for remote mode without -url")
	}
	if !strings.Contains(err.Error(), "-url is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidationBeforeWrite_InvalidMode(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfgPath := filepath.Join(dir, ".claude.json")

	err := Run([]string{"-mode", "badmode", "-binary", "/fake/pasteai"})
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
	if _, statErr := os.Stat(cfgPath); !os.IsNotExist(statErr) {
		t.Error("claude.json should not be created on validation error")
	}
}

// --- Doctor unit tests ---

func TestDoctorAllPass(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	fakeCmd, err := os.CreateTemp(dir, "pasteai-fake-*")
	if err != nil {
		t.Fatal(err)
	}
	fakeCmd.Close()
	if err := os.Chmod(fakeCmd.Name(), 0755); err != nil {
		t.Fatal(err)
	}

	cfg := map[string]any{
		"mcpServers": map[string]any{
			"pasteai": map[string]any{
				"command": fakeCmd.Name(),
				"args":    []any{"mcp"},
			},
		},
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, ".claude.json"), append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}

	if err := RunDoctor(nil); err != nil {
		t.Errorf("expected all checks to pass, got: %v", err)
	}
}

func TestDoctorMissingConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	if err := RunDoctor(nil); err == nil {
		t.Error("expected error when config missing")
	}
}

func TestDoctorMissingEntry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	cfg := map[string]any{
		"mcpServers": map[string]any{
			"other": map[string]any{"command": "/usr/bin/other", "args": []any{"serve"}},
		},
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	os.WriteFile(filepath.Join(dir, ".claude.json"), append(data, '\n'), 0600)

	if err := RunDoctor(nil); err == nil {
		t.Error("expected error when pasteai entry missing")
	}
}

func TestDoctorBadCommandPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	cfg := map[string]any{
		"mcpServers": map[string]any{
			"pasteai": map[string]any{
				"command": "/nonexistent/pasteai-xyz-doesnotexist",
				"args":    []any{"mcp"},
			},
		},
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	os.WriteFile(filepath.Join(dir, ".claude.json"), append(data, '\n'), 0600)

	if err := RunDoctor(nil); err == nil {
		t.Error("expected error when command path does not exist")
	}
}

func TestDoctorServerUnreachable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	fakeCmd, _ := os.CreateTemp(dir, "pasteai-fake-*")
	fakeCmd.Close()
	os.Chmod(fakeCmd.Name(), 0755)

	// Bind then immediately close so the port is in connection-refused state.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	serverURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	cfg := map[string]any{
		"mcpServers": map[string]any{
			"pasteai": map[string]any{
				"command": fakeCmd.Name(),
				"args":    []any{"mcp"},
				"env":     map[string]any{"PASTEAI_URL": serverURL},
			},
		},
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	os.WriteFile(filepath.Join(dir, ".claude.json"), append(data, '\n'), 0600)

	if err := RunDoctor(nil); err == nil {
		t.Error("expected error when server unreachable")
	}
}

func TestDoctorRestartHint(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	fakeCmd, _ := os.CreateTemp(dir, "pasteai-fake-*")
	fakeCmd.Close()
	os.Chmod(fakeCmd.Name(), 0755)

	cfg := map[string]any{
		"mcpServers": map[string]any{
			"pasteai": map[string]any{
				"command": fakeCmd.Name(),
				"args":    []any{"mcp"},
			},
		},
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	// File just written → mtime < 5 minutes → restart hint fires (non-failing)
	os.WriteFile(filepath.Join(dir, ".claude.json"), append(data, '\n'), 0600)

	if err := RunDoctor(nil); err != nil {
		t.Errorf("expected pass with restart hint, got: %v", err)
	}
}

// --- Kiro tests ---

func getKiroEntryUnit(t *testing.T, cfg map[string]any) map[string]any {
	t.Helper()
	servers, ok := cfg["mcpServers"].(map[string]any)
	if !ok {
		t.Fatal("mcpServers key missing or wrong type")
	}
	entry, ok := servers["pasteai"].(map[string]any)
	if !ok {
		t.Fatal("pasteai entry missing or wrong type")
	}
	return entry
}

func TestKiroMergeEmbedded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")

	action, err := mergeClaudeJSON(path, "/fake/pasteai", modeEmbedded, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if action != "Added" {
		t.Errorf("action = %s, want Added", action)
	}
	entry := getKiroEntryUnit(t, readJSONFile(t, path))
	if entry["command"] != "/fake/pasteai" {
		t.Errorf("command = %v", entry["command"])
	}
	if _, hasEnv := entry["env"]; hasEnv {
		t.Error("embedded mode should have no env block")
	}
}

func TestKiroMergeRemote(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")

	_, err := mergeClaudeJSON(path, "/fake/pasteai", modeRemote, "https://pasteai.io", "mykey")
	if err != nil {
		t.Fatal(err)
	}
	entry := getKiroEntryUnit(t, readJSONFile(t, path))
	env, ok := entry["env"].(map[string]any)
	if !ok {
		t.Fatal("remote mode must have env block")
	}
	if env["PASTEAI_URL"] != "https://pasteai.io" {
		t.Errorf("PASTEAI_URL = %v", env["PASTEAI_URL"])
	}
	if env["PASTEAI_API_KEY"] != "mykey" {
		t.Errorf("PASTEAI_API_KEY = %v", env["PASTEAI_API_KEY"])
	}
}

func TestKiroDirCreated(t *testing.T) {
	dir := t.TempDir()
	// Nested path that doesn't exist yet — mergeClaudeJSON must create it.
	path := filepath.Join(dir, ".kiro", "settings", "mcp.json")

	_, err := mergeClaudeJSON(path, "/fake/pasteai", modeEmbedded, "", "")
	if err != nil {
		t.Fatalf("expected parent dirs to be created: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("mcp.json should exist after merge")
	}
}

// --- opencode tests ---

func getOpenCodeEntryUnit(t *testing.T, cfg map[string]any) map[string]any {
	t.Helper()
	mcp, ok := cfg["mcp"].(map[string]any)
	if !ok {
		t.Fatal("mcp key missing or wrong type")
	}
	entry, ok := mcp["pasteai"].(map[string]any)
	if !ok {
		t.Fatal("pasteai entry missing or wrong type")
	}
	return entry
}

func TestOpenCodeMergeEmbedded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")

	action, err := mergeOpencodeJSON(path, "/fake/pasteai", modeEmbedded, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if action != "Added" {
		t.Errorf("action = %s, want Added", action)
	}
	entry := getOpenCodeEntryUnit(t, readJSONFile(t, path))
	if entry["type"] != "local" {
		t.Errorf("type = %v, want local", entry["type"])
	}
	cmd, _ := entry["command"].([]any)
	if len(cmd) < 2 || cmd[0] != "/fake/pasteai" || cmd[1] != "mcp" {
		t.Errorf("command = %v, want [\"/fake/pasteai\", \"mcp\"]", cmd)
	}
	if _, hasEnv := entry["environment"]; hasEnv {
		t.Error("embedded mode should have no environment block")
	}
}

func TestOpenCodeMergeLocal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")

	_, err := mergeOpencodeJSON(path, "/fake/pasteai", modeLocal, "", "")
	if err != nil {
		t.Fatal(err)
	}
	entry := getOpenCodeEntryUnit(t, readJSONFile(t, path))
	env, ok := entry["environment"].(map[string]any)
	if !ok {
		t.Fatalf("local mode must have environment block, got %v", entry)
	}
	if env["PASTEAI_URL"] != "http://localhost:8080" {
		t.Errorf("PASTEAI_URL = %v", env["PASTEAI_URL"])
	}
}

func TestOpenCodeMergeRemoteWithKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")

	_, err := mergeOpencodeJSON(path, "/fake/pasteai", modeRemote, "https://pasteai.io", "mykey")
	if err != nil {
		t.Fatal(err)
	}
	entry := getOpenCodeEntryUnit(t, readJSONFile(t, path))
	env, ok := entry["environment"].(map[string]any)
	if !ok {
		t.Fatal("remote mode must have environment block")
	}
	if env["PASTEAI_URL"] != "https://pasteai.io" {
		t.Errorf("PASTEAI_URL = %v", env["PASTEAI_URL"])
	}
	if env["PASTEAI_API_KEY"] != "mykey" {
		t.Errorf("PASTEAI_API_KEY = %v", env["PASTEAI_API_KEY"])
	}
}

func TestOpenCodeMergeRemoteNoKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")

	_, err := mergeOpencodeJSON(path, "/fake/pasteai", modeRemote, "https://pasteai.io", "")
	if err != nil {
		t.Fatal(err)
	}
	entry := getOpenCodeEntryUnit(t, readJSONFile(t, path))
	env, ok := entry["environment"].(map[string]any)
	if !ok {
		t.Fatal("remote mode must have environment block")
	}
	if _, hasKey := env["PASTEAI_API_KEY"]; hasKey {
		t.Error("PASTEAI_API_KEY should be absent when not provided")
	}
}

func TestOpenCodePreservesOtherKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")

	existing := map[string]any{
		"$schema": "https://opencode.ai/config.json",
		"mcp": map[string]any{
			"other-tool": map[string]any{"type": "local", "command": []any{"other"}},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(path, append(data, '\n'), 0600)

	_, err := mergeOpencodeJSON(path, "/fake/pasteai", modeEmbedded, "", "")
	if err != nil {
		t.Fatal(err)
	}

	result := readJSONFile(t, path)
	if result["$schema"] == nil {
		t.Error("$schema key was removed")
	}
	mcp := result["mcp"].(map[string]any)
	if _, ok := mcp["other-tool"]; !ok {
		t.Error("other-tool was removed")
	}
	if _, ok := mcp["pasteai"]; !ok {
		t.Error("pasteai was not added")
	}
}

func TestOpenCodeMergeIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")

	if _, err := mergeOpencodeJSON(path, "/fake/pasteai", modeEmbedded, "", ""); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)

	if _, err := mergeOpencodeJSON(path, "/fake/pasteai", modeEmbedded, "", ""); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)

	if !bytes.Equal(first, second) {
		t.Errorf("idempotency failure:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestOpenCodeUpdatedAction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")

	if _, err := mergeOpencodeJSON(path, "/fake/pasteai", modeEmbedded, "", ""); err != nil {
		t.Fatal(err)
	}
	action, err := mergeOpencodeJSON(path, "/fake/pasteai", modeRemote, "https://pasteai.io", "")
	if err != nil {
		t.Fatal(err)
	}
	if action != "Updated" {
		t.Errorf("action = %s, want Updated", action)
	}
}

func TestOpenCodePreservesExtraEntryFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")

	existing := map[string]any{
		"mcp": map[string]any{
			"pasteai": map[string]any{
				"type":       "local",
				"command":    []any{"/old/pasteai", "mcp"},
				"user_field": "keep-me",
			},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(path, append(data, '\n'), 0600)

	_, err := mergeOpencodeJSON(path, "/new/pasteai", modeEmbedded, "", "")
	if err != nil {
		t.Fatal(err)
	}
	entry := getOpenCodeEntryUnit(t, readJSONFile(t, path))
	cmd, _ := entry["command"].([]any)
	if len(cmd) == 0 || cmd[0] != "/new/pasteai" {
		t.Errorf("command not updated: %v", entry["command"])
	}
	if entry["user_field"] != "keep-me" {
		t.Errorf("user_field was removed: %v", entry)
	}
}

func TestOpenCodePreservesUserEnvVars(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")

	existing := map[string]any{
		"mcp": map[string]any{
			"pasteai": map[string]any{
				"type":    "local",
				"command": []any{"/old/pasteai", "mcp"},
				"environment": map[string]any{
					"PASTEAI_URL":    "http://old:8080",
					"MY_CUSTOM_FLAG": "yes",
				},
			},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(path, append(data, '\n'), 0600)

	_, err := mergeOpencodeJSON(path, "/new/pasteai", modeLocal, "", "")
	if err != nil {
		t.Fatal(err)
	}
	entry := getOpenCodeEntryUnit(t, readJSONFile(t, path))
	env, ok := entry["environment"].(map[string]any)
	if !ok {
		t.Fatal("environment block missing")
	}
	if env["PASTEAI_URL"] != "http://localhost:8080" {
		t.Errorf("PASTEAI_URL not updated: %v", env["PASTEAI_URL"])
	}
	if env["MY_CUSTOM_FLAG"] != "yes" {
		t.Errorf("MY_CUSTOM_FLAG was removed: %v", env)
	}
}

func TestOpenCodeSwitchToEmbeddedRemovesURLKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")

	existing := map[string]any{
		"mcp": map[string]any{
			"pasteai": map[string]any{
				"type":    "local",
				"command": []any{"/old/pasteai", "mcp"},
				"environment": map[string]any{
					"PASTEAI_URL":     "https://remote.example.com",
					"PASTEAI_API_KEY": "secret",
					"MY_CUSTOM_FLAG":  "yes",
				},
			},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(path, append(data, '\n'), 0600)

	_, err := mergeOpencodeJSON(path, "/new/pasteai", modeEmbedded, "", "")
	if err != nil {
		t.Fatal(err)
	}
	entry := getOpenCodeEntryUnit(t, readJSONFile(t, path))
	env, ok := entry["environment"].(map[string]any)
	if !ok {
		t.Fatal("environment block missing (expected because MY_CUSTOM_FLAG survived)")
	}
	if _, has := env["PASTEAI_URL"]; has {
		t.Error("PASTEAI_URL should be removed when switching to embedded")
	}
	if _, has := env["PASTEAI_API_KEY"]; has {
		t.Error("PASTEAI_API_KEY should be removed when switching to embedded")
	}
	if env["MY_CUSTOM_FLAG"] != "yes" {
		t.Errorf("MY_CUSTOM_FLAG was removed: %v", env)
	}
}

func TestOpenCodeDirCreated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".config", "opencode", "opencode.json")

	_, err := mergeOpencodeJSON(path, "/fake/pasteai", modeEmbedded, "", "")
	if err != nil {
		t.Fatalf("expected parent dirs to be created: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("opencode.json should exist after merge")
	}
}

// --- Binary path tests ---

func TestSelfPathHomebrew(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "pasteai-real")
	if err := os.WriteFile(target, []byte{}, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "pasteai-link")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks not supported on this platform")
	}

	// Use dir as a fake Homebrew Cellar prefix
	resolved, err := selfPathFrom(link, []string{dir + "/"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved == link {
		t.Error("expected symlink to be resolved for Homebrew-prefixed path")
	}
}

func TestSelfPathNix(t *testing.T) {
	nixPath := "/nix/store/abc123-pasteai/bin/pasteai"
	result, err := selfPathFrom(nixPath, homebrewPrefixes)
	if err != nil {
		t.Fatal(err)
	}
	if result != nixPath {
		t.Errorf("nix path should return unchanged, got %s", result)
	}
}

func TestBinaryFlagRelativePath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	if err := Run([]string{"-mode", "embedded", "-binary", "./pasteai"}); err != nil {
		t.Fatal(err)
	}

	entry := getPasteaiEntryUnit(t, readJSONFile(t, filepath.Join(dir, ".claude.json")))
	cmd, _ := entry["command"].(string)
	if !filepath.IsAbs(cmd) {
		t.Errorf("command should be absolute, got %s", cmd)
	}
}

func TestRunLocalMode(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	if err := Run([]string{"-mode", "local", "-binary", "/fake/pasteai"}); err != nil {
		t.Fatalf("Run local: %v", err)
	}
}

func TestBinaryFlagNoStat(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	nonExistent := "/nonexistent/path/to/pasteai-xyz"
	if err := Run([]string{"-mode", "embedded", "-binary", nonExistent}); err != nil {
		t.Fatalf("setup with nonexistent -binary should succeed: %v", err)
	}

	entry := getPasteaiEntryUnit(t, readJSONFile(t, filepath.Join(dir, ".claude.json")))
	if entry["command"] != nonExistent {
		t.Errorf("command = %v, want %s", entry["command"], nonExistent)
	}
}

// ── checkServer ────────────────────────────────────────────

func TestCheckServer_EmptyURL(t *testing.T) {
	if checkServer("") {
		t.Error("expected false for empty server URL")
	}
}

func TestCheckServer_OK(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	if !checkServer(ts.URL) {
		t.Error("expected true when server returns 200 OK")
	}
}

func TestCheckServer_NonOK(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer ts.Close()
	if checkServer(ts.URL) {
		t.Error("expected false when server returns non-200")
	}
}

// ── detectMode ─────────────────────────────────────────────

func TestDetectMode_Local(t *testing.T) {
	entry := map[string]any{
		"env": map[string]any{"PASTEAI_URL": "http://localhost:8080"},
	}
	if m := detectMode(entry); m != modeLocal {
		t.Errorf("detectMode = %q, want %q", m, modeLocal)
	}
}

func TestDetectMode_EmbeddedEmptyURL(t *testing.T) {
	entry := map[string]any{
		"env": map[string]any{"PASTEAI_URL": ""},
	}
	if m := detectMode(entry); m != modeEmbedded {
		t.Errorf("detectMode = %q, want %q", m, modeEmbedded)
	}
}

// ── checkEntry ──────────────────────────────────────────────

func TestCheckEntry_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude.json")
	os.WriteFile(path, []byte("not valid json"), 0600)

	_, _, _, err := checkEntry(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestCheckEntry_NoMCPServers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude.json")
	data, _ := json.Marshal(map[string]any{"other": "value"})
	os.WriteFile(path, data, 0600)

	_, _, _, err := checkEntry(path)
	if err == nil {
		t.Fatal("expected error when mcpServers missing")
	}
}

// ── Run error paths ─────────────────────────────────────────

func TestRun_ParseError(t *testing.T) {
	if err := Run([]string{"--unknown-flag-xyz"}); err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

func TestRunWithoutBinaryFlag(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	// No -binary flag — exercises selfPath() call inside Run.
	if err := Run([]string{"-mode", "embedded"}); err != nil {
		t.Fatalf("Run without -binary: %v", err)
	}
}

func TestRunMergeJSONError(t *testing.T) {
	dir := t.TempDir()
	// Place a directory where .claude.json should be — os.ReadFile on a directory
	// returns EISDIR (not ErrNotExist), hitting the error fallback path in Run.
	if err := os.Mkdir(filepath.Join(dir, ".claude.json"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", dir)

	err := Run([]string{"-mode", "embedded", "-binary", "/fake/pasteai"})
	if err == nil {
		t.Fatal("expected error when .claude.json is a directory")
	}
}
