package setup

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	modeEmbedded = "embedded"
	modeLocal    = "local"
	modeRemote   = "remote"
)

var homebrewPrefixes = []string{
	"/opt/homebrew/Cellar/",
	"/usr/local/Cellar/",
	"/home/linuxbrew/.linuxbrew/Cellar/",
}

func userLabel(mode string) string {
	switch mode {
	case modeEmbedded:
		return "automatic"
	case modeLocal:
		return "manual"
	default:
		return mode
	}
}

func parseMode(s string) (string, bool) {
	switch strings.ToLower(s) {
	case "automatic", "embedded":
		return modeEmbedded, true
	case "manual", "local":
		return modeLocal, true
	case "remote":
		return modeRemote, true
	default:
		return "", false
	}
}

// Run implements `pasteai setup`.
func Run(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	flagMode := fs.String("mode", "", "How PasteAI should run: automatic, manual, or remote")
	flagURL := fs.String("url", "", "Server URL (required when mode=remote)")
	flagAPIKey := fs.String("api-key", "", "API key (optional, for mode=remote)")
	flagBinary := fs.String("binary", "", "Override binary path written to config files")

	if err := fs.Parse(args); err != nil {
		return err
	}

	mode := *flagMode
	url := *flagURL
	apiKey := *flagAPIKey

	if mode == "" {
		tty, err := openTTY()
		if err != nil {
			return fmt.Errorf("not running in a terminal; use -mode to specify mode")
		}
		defer tty.Close()
		iMode, iURL, iKey, err := promptUser(tty)
		if err != nil {
			return err
		}
		mode, url, apiKey = iMode, iURL, iKey
	}

	// Validate before touching any file.
	internalMode, ok := parseMode(mode)
	if !ok {
		return fmt.Errorf("unknown mode %q; valid: automatic, manual, remote", mode)
	}
	if internalMode == modeRemote && url == "" {
		return fmt.Errorf("-url is required when mode=remote")
	}

	var binaryPath string
	if *flagBinary != "" {
		abs, err := filepath.Abs(*flagBinary)
		if err != nil {
			return fmt.Errorf("resolving -binary path: %w", err)
		}
		binaryPath = abs
	} else {
		p, err := selfPath()
		if err != nil {
			return fmt.Errorf("detecting binary path: %w", err)
		}
		binaryPath = p
	}

	if os.Getuid() == 0 && os.Getenv("SUDO_USER") != "" {
		fmt.Fprintf(os.Stderr, "warning: running as root; config files will be written to root's home directory.\nIf you meant to configure for user %q, run without sudo.\n", os.Getenv("SUDO_USER"))
	}

	// Claude Code — primary; fail hard if it can't be written.
	cfgPath, err := claudeJSONPath()
	if err != nil {
		return err
	}
	action, err := mergeClaudeJSON(cfgPath, binaryPath, internalMode, url, apiKey)
	if err != nil {
		fallback := map[string]any{"mcpServers": map[string]any{"pasteai": mergeEntry(nil, binaryPath, internalMode, url, apiKey)}}
		fb, _ := json.Marshal(fallback)
		fmt.Fprintf(os.Stderr, "✗ Could not write %s: %v\n  Add this to ~/.claude.json manually:\n  %s\n", cfgPath, err, fb)
		return err
	}
	fmt.Printf("✓ %s pasteai in %s (Claude Code, mode: %s)\n", action, cfgPath, userLabel(internalMode))

	// Kiro — same mcpServers format as Claude Code.
	if kiroPath, kiroErr := kiroJSONPath(); kiroErr == nil {
		if kiroAction, kiroErr := mergeClaudeJSON(kiroPath, binaryPath, internalMode, url, apiKey); kiroErr != nil {
			fmt.Fprintf(os.Stderr, "✗ Could not write %s: %v\n", kiroPath, kiroErr)
		} else {
			fmt.Printf("✓ %s pasteai in %s (Kiro, mode: %s)\n", kiroAction, kiroPath, userLabel(internalMode))
		}
	}

	// opencode — uses {"mcp": {...}} with a different entry shape.
	if opencodePath, opencodeErr := opencodeConfigPath(); opencodeErr == nil {
		if opencodeAction, opencodeErr := mergeOpencodeJSON(opencodePath, binaryPath, internalMode, url, apiKey); opencodeErr != nil {
			fmt.Fprintf(os.Stderr, "✗ Could not write %s: %v\n", opencodePath, opencodeErr)
		} else {
			fmt.Printf("✓ %s pasteai in %s (opencode, mode: %s)\n", opencodeAction, opencodePath, userLabel(internalMode))
		}
	}

	fmt.Print("\nNext steps:\n")
	if internalMode == modeLocal {
		fmt.Print("  1. Start the server now:   pasteai serve\n")
		fmt.Print("     (Keep this terminal open, or set up autostart:\n")
		fmt.Print("      https://github.com/pasteai/pasteai/blob/main/docs/server-setup.md)\n")
		fmt.Print("  2. Quit and reopen your AI editor\n")
		fmt.Print("  3. Say: \"Write a short summary and publish it as a PasteAI document\"\n")
		fmt.Print("     You'll get a link — that means it's working.\n")
	} else {
		fmt.Print("  1. Quit and reopen your AI editor\n")
		fmt.Print("  2. Say: \"Write a short summary and publish it as a PasteAI document\"\n")
		fmt.Print("     You'll get a link — that means it's working.\n")
	}
	fmt.Print("\nIf something seems wrong, run: pasteai doctor\n")

	return nil
}

// mergeClaudeJSON performs the JSON merge. Returns "Added" or "Updated".
func mergeClaudeJSON(cfgPath, binaryPath, mode, url, apiKey string) (string, error) {
	cfg, err := readClaudeJSON(cfgPath)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", cfgPath, err)
	}

	servers, _ := cfg["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}

	existing, _ := servers["pasteai"].(map[string]any)
	action := "Added"
	if existing != nil {
		action = "Updated"
	}
	servers["pasteai"] = mergeEntry(existing, binaryPath, mode, url, apiKey)
	cfg["mcpServers"] = servers

	if err := writeClaudeJSON(cfgPath, cfg); err != nil {
		return "", err
	}
	return action, nil
}

// mergeEntry builds a Claude Code / Kiro MCP entry, preserving any fields or
// env vars the user added that setup does not own.
func mergeEntry(existing map[string]any, binaryPath, mode, url, apiKey string) map[string]any {
	entry := cloneMap(existing)
	entry["command"] = binaryPath
	entry["args"] = []string{"mcp"}

	existingEnv, _ := existing["env"].(map[string]any)
	env := cloneMap(existingEnv)
	delete(env, "PASTEAI_URL")
	delete(env, "PASTEAI_API_KEY")
	switch mode {
	case modeLocal:
		env["PASTEAI_URL"] = "http://localhost:8080"
	case modeRemote:
		env["PASTEAI_URL"] = url
		if apiKey != "" {
			env["PASTEAI_API_KEY"] = apiKey
		}
	}
	if len(env) > 0 {
		entry["env"] = env
	} else {
		delete(entry, "env")
	}
	return entry
}

func cloneMap(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

func promptUser(tty *os.File) (mode, url, key string, err error) {
	r := bufio.NewReader(tty)

	fmt.Fprint(tty, "How should PasteAI run?\n\n")
	fmt.Fprint(tty, "  1) Automatic  — PasteAI starts in the background when Claude needs it\n")
	fmt.Fprint(tty, "                  (recommended for Claude Code on this machine)\n")
	fmt.Fprint(tty, "  2) Manual     — You run 'pasteai serve' yourself on this machine\n")
	fmt.Fprint(tty, "  3) Remote     — Connect to a PasteAI server running elsewhere\n\n")
	fmt.Fprint(tty, "Enter 1-3 [1]: ")

	line, err := r.ReadString('\n')
	if err != nil {
		return "", "", "", fmt.Errorf("reading input: %w", err)
	}
	choice := strings.TrimSpace(line)
	if choice == "" {
		choice = "1"
	}

	switch choice {
	case "1":
		return modeEmbedded, "", "", nil
	case "2":
		return modeLocal, "", "", nil
	case "3":
		fmt.Fprint(tty, "Server URL (e.g. https://pasteai.yourcompany.com): ")
		urlLine, err := r.ReadString('\n')
		if err != nil {
			return "", "", "", fmt.Errorf("reading URL: %w", err)
		}
		url = strings.TrimSpace(urlLine)
		if url == "" {
			return "", "", "", fmt.Errorf("-url is required when mode=remote")
		}
		fmt.Fprint(tty, "API key (press Enter to skip): ")
		keyLine, err := r.ReadString('\n')
		if err != nil {
			return "", "", "", fmt.Errorf("reading API key: %w", err)
		}
		return modeRemote, url, strings.TrimSpace(keyLine), nil
	default:
		return "", "", "", fmt.Errorf("invalid choice %q; enter 1, 2, or 3", choice)
	}
}

func openTTY() (*os.File, error) {
	if runtime.GOOS == "windows" {
		return os.Open("CONIN$")
	}
	return os.Open("/dev/tty")
}

func selfPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return selfPathFrom(exe, homebrewPrefixes)
}

// selfPathFrom is the testable inner path-resolution logic.
// It resolves symlinks only for known Homebrew Cellar prefixes.
func selfPathFrom(exe string, prefixes []string) (string, error) {
	for _, prefix := range prefixes {
		if strings.HasPrefix(exe, prefix) {
			if resolved, err := filepath.EvalSymlinks(exe); err == nil {
				return resolved, nil
			}
		}
	}
	return exe, nil
}

func claudeJSONPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, ".claude.json"), nil
}

// kiroJSONPath returns ~/.kiro/settings/mcp.json.
// Kiro uses the same mcpServers format as Claude Code.
func kiroJSONPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, ".kiro", "settings", "mcp.json"), nil
}

// opencodeConfigPath returns the opencode config file path.
// Respects XDG_CONFIG_HOME; defaults to ~/.config/opencode/opencode.json.
func opencodeConfigPath() (string, error) {
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("getting home directory: %w", err)
		}
		configDir = filepath.Join(home, ".config")
	}
	return filepath.Join(configDir, "opencode", "opencode.json"), nil
}

// mergeOpenCodeEntry builds an opencode MCP entry, preserving any fields or
// environment vars the user added that setup does not own.
// opencode uses {"type":"local","command":[binary,args...],"environment":{...}}
// rather than the standard {"command":binary,"args":[...],"env":{...}}.
func mergeOpenCodeEntry(existing map[string]any, binaryPath, mode, url, apiKey string) map[string]any {
	entry := cloneMap(existing)
	entry["type"] = "local"
	entry["command"] = []string{binaryPath, "mcp"}

	existingEnv, _ := existing["environment"].(map[string]any)
	env := cloneMap(existingEnv)
	delete(env, "PASTEAI_URL")
	delete(env, "PASTEAI_API_KEY")
	switch mode {
	case modeLocal:
		env["PASTEAI_URL"] = "http://localhost:8080"
	case modeRemote:
		env["PASTEAI_URL"] = url
		if apiKey != "" {
			env["PASTEAI_API_KEY"] = apiKey
		}
	}
	if len(env) > 0 {
		entry["environment"] = env
	} else {
		delete(entry, "environment")
	}
	return entry
}

// mergeOpencodeJSON writes the pasteai entry into the opencode config file.
// opencode uses {"mcp": {"pasteai": {...}}} rather than {"mcpServers": {"pasteai": {...}}}.
func mergeOpencodeJSON(cfgPath, binaryPath, mode, url, apiKey string) (string, error) {
	cfg, err := readClaudeJSON(cfgPath)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", cfgPath, err)
	}

	mcp, _ := cfg["mcp"].(map[string]any)
	if mcp == nil {
		mcp = map[string]any{}
	}

	existing, _ := mcp["pasteai"].(map[string]any)
	action := "Added"
	if existing != nil {
		action = "Updated"
	}
	mcp["pasteai"] = mergeOpenCodeEntry(existing, binaryPath, mode, url, apiKey)
	cfg["mcp"] = mcp

	if err := writeClaudeJSON(cfgPath, cfg); err != nil {
		return "", err
	}
	return action, nil
}

func readClaudeJSON(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		// Malformed JSON — treat as empty rather than blocking setup
		return map[string]any{}, nil
	}
	return cfg, nil
}

func writeClaudeJSON(path string, cfg map[string]any) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	// Write to a temp file in the same directory then rename for atomicity.
	// A crash between truncate and write would otherwise corrupt the file.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".pasteai.tmp*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil {
		os.Remove(tmp)
		return werr
	}
	if cerr != nil {
		os.Remove(tmp)
		return cerr
	}
	if err := os.Chmod(tmp, 0600); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
