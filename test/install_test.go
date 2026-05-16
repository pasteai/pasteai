package test

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"testing"
)

func TestInstallBinaryFunctional(t *testing.T) {
	requireInstall(t)
	home := "/tmp/test-binary"
	dockerExec(installContainer, "mkdir", "-p", home)

	if _, err := dockerInstall(home, "", ""); err != nil {
		t.Fatalf("install.sh failed: %v", err)
	}

	out, err := dockerExecOutput(installContainer, home+"/.local/bin/pasteai", "version")
	if err != nil {
		t.Fatalf("pasteai version: %v\n%s", err, out)
	}
	if out == "" {
		t.Error("empty version output")
	}
}

func TestInstallOnPath(t *testing.T) {
	requireInstall(t)
	home := "/tmp/test-onpath"
	dockerExec(installContainer, "mkdir", "-p", home)

	if _, err := dockerInstall(home, "", ""); err != nil {
		t.Fatalf("install.sh failed: %v", err)
	}

	// install.sh appends to ~/.bashrc; verify the install dir appears there
	_, err := dockerExecOutput(installContainer, "grep", "-q", home+"/.local/bin", home+"/.bashrc")
	if err != nil {
		t.Errorf("PATH not written to ~/.bashrc")
	}
}

func TestInstallServerStarts(t *testing.T) {
	requireInstall(t)
	home := "/tmp/test-server"
	dockerExec(installContainer, "mkdir", "-p", home)

	if _, err := dockerInstall(home, "", ""); err != nil {
		t.Fatalf("install.sh failed: %v", err)
	}

	// Start serve in background; poll until ready or timeout.
	script := fmt.Sprintf(`
		%s serve &
		for i in $(seq 1 20); do
			if curl -sf http://localhost:8080/api/documents >/dev/null 2>&1; then
				kill %%1 2>/dev/null; exit 0
			fi
			sleep 0.5
		done
		exit 1
	`, home+"/.local/bin/pasteai")

	out, err := exec.Command("docker", "exec",
		"-e", "HOME="+home,
		installContainer,
		"bash", "-c", script,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("server did not start within 10s: %v\n%s", err, out)
	}
}

func TestInstallModeEmbedded(t *testing.T) {
	requireInstall(t)
	home := "/tmp/test-mode-embedded"
	dockerExec(installContainer, "mkdir", "-p", home)

	if _, err := dockerInstall(home, "", ""); err != nil {
		t.Fatalf("install.sh failed: %v", err)
	}

	raw, err := dockerExecOutput(installContainer, "cat", home+"/.claude.json")
	if err != nil {
		t.Fatalf("read claude.json: %v", err)
	}
	entry := parseClaudeEntry(t, raw)
	if _, hasEnv := entry["env"]; hasEnv {
		t.Errorf("embedded mode should have no env block, got: %v", entry)
	}
}

func TestInstallModeLocal(t *testing.T) {
	requireInstall(t)
	home := "/tmp/test-mode-local"
	dockerExec(installContainer, "mkdir", "-p", home)

	if _, err := dockerInstall(home, "local", ""); err != nil {
		t.Fatalf("install.sh failed: %v", err)
	}

	raw, err := dockerExecOutput(installContainer, "cat", home+"/.claude.json")
	if err != nil {
		t.Fatalf("read claude.json: %v", err)
	}
	entry := parseClaudeEntry(t, raw)
	env, ok := entry["env"].(map[string]any)
	if !ok {
		t.Fatalf("local mode must have env block, got: %v", entry)
	}
	if env["PASTEAI_URL"] != "http://localhost:8080" {
		t.Errorf("PASTEAI_URL = %v, want http://localhost:8080", env["PASTEAI_URL"])
	}
}

func TestInstallModeRemote(t *testing.T) {
	requireInstall(t)
	home := "/tmp/test-mode-remote"
	dockerExec(installContainer, "mkdir", "-p", home)

	if _, err := dockerInstall(home, "remote", "https://example.com"); err != nil {
		t.Fatalf("install.sh failed: %v", err)
	}

	raw, err := dockerExecOutput(installContainer, "cat", home+"/.claude.json")
	if err != nil {
		t.Fatalf("read claude.json: %v", err)
	}
	entry := parseClaudeEntry(t, raw)
	env, ok := entry["env"].(map[string]any)
	if !ok {
		t.Fatalf("remote mode must have env block, got: %v", entry)
	}
	if env["PASTEAI_URL"] != "https://example.com" {
		t.Errorf("PASTEAI_URL = %v, want https://example.com", env["PASTEAI_URL"])
	}
}

func TestInstallModeRemoteNoURL(t *testing.T) {
	requireInstall(t)
	home := "/tmp/test-mode-remote-nourl"
	dockerExec(installContainer, "mkdir", "-p", home)

	if _, err := dockerInstall(home, "remote", ""); err == nil {
		t.Error("expected install.sh to exit non-zero for remote mode without PASTEAI_URL")
	}

	// claude.json must not exist after a failed install
	if _, err := dockerExecOutput(installContainer, "test", "-f", home+"/.claude.json"); err == nil {
		t.Error("claude.json should not exist after failed install")
	}
}

func TestInstallNonDestructiveMerge(t *testing.T) {
	requireInstall(t)
	home := "/tmp/test-merge"
	dockerExec(installContainer, "mkdir", "-p", home)

	// Pre-seed claude.json with an existing server entry
	existing := `{"mcpServers":{"other-tool":{"command":"/usr/bin/other","args":["serve"]}}}`
	exec.Command("docker", "exec", installContainer,
		"bash", "-c", "echo '"+existing+"' > "+home+"/.claude.json",
	).Run()

	if _, err := dockerInstall(home, "", ""); err != nil {
		t.Fatalf("install.sh failed: %v", err)
	}

	raw, err := dockerExecOutput(installContainer, "cat", home+"/.claude.json")
	if err != nil {
		t.Fatalf("read claude.json: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("parse claude.json: %v\n%s", err, raw)
	}
	servers := result["mcpServers"].(map[string]any)
	if _, ok := servers["other-tool"]; !ok {
		t.Error("other-tool was removed (non-destructive merge failed)")
	}
	if _, ok := servers["pasteai"]; !ok {
		t.Error("pasteai was not added")
	}
}

func parseClaudeEntry(t *testing.T, raw string) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("parse claude.json: %v\nraw: %s", err, raw)
	}
	servers, ok := result["mcpServers"].(map[string]any)
	if !ok {
		t.Fatal("mcpServers missing or wrong type")
	}
	entry, ok := servers["pasteai"].(map[string]any)
	if !ok {
		t.Fatal("pasteai entry missing or wrong type")
	}
	return entry
}
