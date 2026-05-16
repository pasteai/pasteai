package setup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
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
