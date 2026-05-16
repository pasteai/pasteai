package setup

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// RunDoctor implements `pasteai doctor`.
func RunDoctor(_ []string) error {
	failed := false

	// Check 1: Binary — always passes if doctor can run.
	exe, exeErr := os.Executable()
	if exeErr != nil {
		fmt.Printf("✗  Binary:  could not determine path: %v\n", exeErr)
		failed = true
	} else if _, err := os.Stat(exe); err != nil {
		fmt.Printf("✗  Binary:  %s — not found\n", exe)
		failed = true
	} else {
		fmt.Printf("✓  Binary:  %s\n", exe)
	}

	// Check 2: Config file
	cfgPath, pathErr := claudeJSONPath()
	if pathErr != nil {
		fmt.Printf("✗  Config:  could not determine path: %v\n", pathErr)
		doctorSkips("config path error")
		fmt.Println("\nFix: run 'pasteai setup'")
		return fmt.Errorf("doctor: checks failed")
	}

	cfgStat, statErr := os.Stat(cfgPath)
	if statErr != nil {
		fmt.Printf("✗  Config:  %s not found\n", cfgPath)
		doctorSkips("config not found")
		fmt.Println("\nFix: run 'pasteai setup'")
		return fmt.Errorf("doctor: checks failed")
	}
	fmt.Printf("✓  Config:  %s\n", cfgPath)

	// Check 3: Entry
	_, entry, entryMode, entryErr := checkEntry(cfgPath)
	if entryErr != nil {
		fmt.Printf("✗  Entry:   %v\n", entryErr)
		fmt.Println("SKIP Command: (entry check failed)")
		fmt.Println("SKIP Server:  (entry check failed)")
		fmt.Println("\nFix: run 'pasteai setup'")
		return fmt.Errorf("doctor: checks failed")
	}
	fmt.Printf("✓  Entry:   mcpServers.pasteai (mode: %s)\n", userLabel(entryMode))

	// Check 4: Command path
	cmdPath, _ := entry["command"].(string)
	if cmdPath == "" {
		fmt.Println("✗  Command: command field is empty")
		fmt.Println("SKIP Server:  (no command path)")
		fmt.Println("\nFix: run 'pasteai setup'")
		return fmt.Errorf("doctor: checks failed")
	}
	if _, err := os.Stat(cmdPath); err != nil {
		fmt.Printf("✗  Command: %s — not found\n", cmdPath)
		if exeErr == nil {
			fmt.Printf("   Fix: run 'pasteai setup -binary %s'\n", exe)
		}
		fmt.Println("SKIP Server:  (command not found)")
		failed = true
	} else {
		fmt.Printf("✓  Command: %s\n", cmdPath)

		// Check 5: Server reachability
		if entryMode == modeEmbedded {
			fmt.Println("--  Server:  automatic mode, no server check")
		} else {
			env, _ := entry["env"].(map[string]any)
			serverURL, _ := env["PASTEAI_URL"].(string)
			if !checkServer(serverURL) {
				failed = true
			}
		}
	}

	// Mtime hint (non-failing)
	if statErr == nil {
		if age := time.Since(cfgStat.ModTime()); age < 5*time.Minute {
			fmt.Printf("!   Restart: config was written %s ago — quit and reopen Claude Code\n", age.Round(time.Second))
		}
	}

	if failed {
		fmt.Println("\nFix: run 'pasteai setup'")
		return fmt.Errorf("doctor: checks failed")
	}
	fmt.Println("\nAll checks passed.")
	return nil
}

func checkEntry(cfgPath string) (cfg map[string]any, entry map[string]any, mode string, err error) {
	data, readErr := os.ReadFile(cfgPath)
	if readErr != nil {
		return nil, nil, "", fmt.Errorf("could not read config: %w", readErr)
	}
	cfg = map[string]any{}
	if parseErr := json.Unmarshal(data, &cfg); parseErr != nil {
		return nil, nil, "", fmt.Errorf("config is not valid JSON")
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	if servers == nil {
		return nil, nil, "", fmt.Errorf("mcpServers key missing")
	}
	entry, _ = servers["pasteai"].(map[string]any)
	if entry == nil {
		return nil, nil, "", fmt.Errorf("mcpServers.pasteai not found")
	}
	mode = detectMode(entry)
	return cfg, entry, mode, nil
}

func detectMode(entry map[string]any) string {
	env, ok := entry["env"].(map[string]any)
	if !ok {
		return modeEmbedded
	}
	urlVal, _ := env["PASTEAI_URL"].(string)
	if urlVal == "" {
		return modeEmbedded
	}
	if urlVal == "http://localhost:8080" {
		return modeLocal
	}
	return modeRemote
}

func checkServer(serverURL string) bool {
	if serverURL == "" {
		fmt.Println("✗  Server:  PASTEAI_URL not set in entry")
		fmt.Println("   Fix: run 'pasteai setup -mode local' or 'pasteai setup -mode remote -url ...'")
		return false
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(serverURL + "/api/documents")
	if err != nil {
		fmt.Printf("✗  Server:  %s — server not running\n", serverURL)
		fmt.Println("   Fix: start it with: pasteai serve")
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) //nolint:errcheck
	if resp.StatusCode == http.StatusOK {
		fmt.Printf("✓  Server:  %s reachable\n", serverURL)
		return true
	}
	fmt.Printf("✗  Server:  %s reachable but returned HTTP %d\n", serverURL, resp.StatusCode)
	return false
}

func doctorSkips(reason string) {
	fmt.Printf("SKIP Entry:   (%s)\n", reason)
	fmt.Printf("SKIP Command: (%s)\n", reason)
	fmt.Printf("SKIP Server:  (%s)\n", reason)
}
