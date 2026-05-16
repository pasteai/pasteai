package test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func runSetup(t *testing.T, mode, binaryFlag, url, apiKey string) (claudeJSON string, err error) {
	t.Helper()
	tmpDir := t.TempDir()

	args := []string{"setup", "-mode", mode, "-binary", binaryFlag}
	if url != "" {
		args = append(args, "-url", url)
	}
	if apiKey != "" {
		args = append(args, "-api-key", apiKey)
	}

	cmd := exec.Command(testBinaryPath, args...)
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	_, err = cmd.Output()
	return filepath.Join(tmpDir, ".claude.json"), err
}

func readClaudeJSON(t *testing.T, path string) map[string]any {
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

func getPasteaiEntry(t *testing.T, cfg map[string]any) map[string]any {
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

func TestSetupEmbedded(t *testing.T) {
	requireBinary(t)
	p, err := runSetup(t, "embedded", "/fake/pasteai", "", "")
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	entry := getPasteaiEntry(t, readClaudeJSON(t, p))
	if _, hasEnv := entry["env"]; hasEnv {
		t.Error("embedded mode should have no env block")
	}
	if entry["command"] != "/fake/pasteai" {
		t.Errorf("command = %v, want /fake/pasteai", entry["command"])
	}
}

func TestSetupLocal(t *testing.T) {
	requireBinary(t)
	p, err := runSetup(t, "local", "/fake/pasteai", "", "")
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	entry := getPasteaiEntry(t, readClaudeJSON(t, p))
	env, ok := entry["env"].(map[string]any)
	if !ok {
		t.Fatalf("local mode must have env block, got: %v", entry)
	}
	if env["PASTEAI_URL"] != "http://localhost:8080" {
		t.Errorf("PASTEAI_URL = %v", env["PASTEAI_URL"])
	}
}

func TestSetupRemoteWithKey(t *testing.T) {
	requireBinary(t)
	p, err := runSetup(t, "remote", "/fake/pasteai", "https://example.com", "mykey")
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	entry := getPasteaiEntry(t, readClaudeJSON(t, p))
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

func TestSetupRemoteNoKey(t *testing.T) {
	requireBinary(t)
	p, err := runSetup(t, "remote", "/fake/pasteai", "https://example.com", "")
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	entry := getPasteaiEntry(t, readClaudeJSON(t, p))
	env, ok := entry["env"].(map[string]any)
	if !ok {
		t.Fatalf("remote mode must have env block")
	}
	if _, hasKey := env["PASTEAI_API_KEY"]; hasKey {
		t.Error("PASTEAI_API_KEY should be absent when not provided")
	}
}

func TestSetupRemoteNoURL(t *testing.T) {
	requireBinary(t)
	tmpDir := t.TempDir()
	claudeJSON := filepath.Join(tmpDir, ".claude.json")

	cmd := exec.Command(testBinaryPath, "setup", "-mode", "remote", "-binary", "/fake/pasteai")
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	if err := cmd.Run(); err == nil {
		t.Error("expected non-zero exit for remote mode without -url")
	}
	if _, err := os.Stat(claudeJSON); !os.IsNotExist(err) {
		t.Error("claude.json should not be created when setup exits with error")
	}
}

func TestSetupInvalidMode(t *testing.T) {
	requireBinary(t)
	tmpDir := t.TempDir()
	claudeJSON := filepath.Join(tmpDir, ".claude.json")

	cmd := exec.Command(testBinaryPath, "setup", "-mode", "badmode", "-binary", "/fake/pasteai")
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	if err := cmd.Run(); err == nil {
		t.Error("expected non-zero exit for invalid mode")
	}
	if _, err := os.Stat(claudeJSON); !os.IsNotExist(err) {
		t.Error("claude.json should not be created on invalid mode")
	}
}

func TestSetupMalformedJSON(t *testing.T) {
	requireBinary(t)
	tmpDir := t.TempDir()
	claudeJSON := filepath.Join(tmpDir, ".claude.json")

	if err := os.WriteFile(claudeJSON, []byte("{ not valid json"), 0600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(testBinaryPath, "setup", "-mode", "embedded", "-binary", "/fake/pasteai")
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("setup should handle malformed JSON: %v", err)
	}
	getPasteaiEntry(t, readClaudeJSON(t, claudeJSON))
}

func TestSetupNonDestructiveMerge(t *testing.T) {
	requireBinary(t)
	tmpDir := t.TempDir()
	claudeJSON := filepath.Join(tmpDir, ".claude.json")

	existing := map[string]any{
		"mcpServers": map[string]any{
			"other-tool": map[string]any{
				"command": "/usr/bin/other",
				"args":    []any{"serve"},
			},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	if err := os.WriteFile(claudeJSON, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(testBinaryPath, "setup", "-mode", "embedded", "-binary", "/fake/pasteai")
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	result := readClaudeJSON(t, claudeJSON)
	servers := result["mcpServers"].(map[string]any)
	if _, ok := servers["other-tool"]; !ok {
		t.Error("other-tool was removed by setup")
	}
	if _, ok := servers["pasteai"]; !ok {
		t.Error("pasteai was not added")
	}
}

func TestSetupIdempotent(t *testing.T) {
	requireBinary(t)
	tmpDir := t.TempDir()
	claudeJSON := filepath.Join(tmpDir, ".claude.json")

	run := func() {
		cmd := exec.Command(testBinaryPath, "setup", "-mode", "embedded", "-binary", "/fake/pasteai")
		cmd.Env = append(os.Environ(), "HOME="+tmpDir)
		if err := cmd.Run(); err != nil {
			t.Fatalf("setup run failed: %v", err)
		}
	}

	run()
	first, _ := os.ReadFile(claudeJSON)
	run()
	second, _ := os.ReadFile(claudeJSON)

	if string(first) != string(second) {
		t.Errorf("idempotency failure:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestSetupModeSwitch(t *testing.T) {
	requireBinary(t)
	tmpDir := t.TempDir()
	claudeJSON := filepath.Join(tmpDir, ".claude.json")

	existing := map[string]any{
		"mcpServers": map[string]any{
			"other-tool": map[string]any{
				"command": "/usr/bin/other",
				"args":    []any{"serve"},
			},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(claudeJSON, append(data, '\n'), 0600)

	runMode := func(mode, url, apiKey string) {
		args := []string{"setup", "-mode", mode, "-binary", "/fake/pasteai"}
		if url != "" {
			args = append(args, "-url", url)
		}
		if apiKey != "" {
			args = append(args, "-api-key", apiKey)
		}
		cmd := exec.Command(testBinaryPath, args...)
		cmd.Env = append(os.Environ(), "HOME="+tmpDir)
		if err := cmd.Run(); err != nil {
			t.Fatalf("setup %s failed: %v", mode, err)
		}
	}

	runMode("embedded", "", "")
	runMode("remote", "https://example.com", "key123")

	result := readClaudeJSON(t, claudeJSON)
	servers := result["mcpServers"].(map[string]any)

	if _, ok := servers["other-tool"]; !ok {
		t.Error("other-tool was removed")
	}

	entry := servers["pasteai"].(map[string]any)
	env, ok := entry["env"].(map[string]any)
	if !ok {
		t.Fatal("remote entry must have env block after mode switch")
	}
	if env["PASTEAI_URL"] != "https://example.com" {
		t.Errorf("PASTEAI_URL = %v", env["PASTEAI_URL"])
	}
	if env["PASTEAI_API_KEY"] != "key123" {
		t.Errorf("PASTEAI_API_KEY = %v", env["PASTEAI_API_KEY"])
	}
}
