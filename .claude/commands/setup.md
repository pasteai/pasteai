Set up PasteAI on this machine: install the binary, optionally start a persistent server, and configure MCP so Claude can publish documents.

If the user invokes this command with arguments (e.g. `/setup remote https://example.com mykey`), read mode, URL, and key from the args and skip Step 0. Otherwise work through each step in order.

Use your Bash tool to run commands. Tell the user what you're doing at each step.

---

## Step 0 — Choose mode (skip if args provided)

Ask the user which mode they want:

- **Embedded** (default) — pasteai starts a local server automatically on first use. Nothing to configure. Best for Claude Code on your laptop.
- **Local server** — you run `pasteai serve` separately (Docker, systemd, etc.). Best when Claude runs in a container or you want the server always running.
- **Remote** — point at an existing PasteAI instance. Ask for the server URL and optionally an API key.

Note the chosen mode (call it PASTEAI_MODE: `embedded`, `local`, or `remote`). If remote, note PASTEAI_URL and PASTEAI_API_KEY. You will use these in Step 3.

---

## Step 1 — Install the binary

First, check if pasteai is already installed anywhere:

```sh
which pasteai 2>/dev/null || \
  test -f "$(go env GOPATH 2>/dev/null)/bin/pasteai" && echo "$(go env GOPATH)/bin/pasteai" || \
  test -f "$HOME/.local/bin/pasteai" && echo "$HOME/.local/bin/pasteai"
```

**If found**, note the full path — call it PASTEAI_BIN. Skip to Step 2.

**If not found**, choose an install method:

### Option A: go install (if Go is available)

```sh
which go
```

If Go is available:
```sh
go install github.com/pasteai/pasteai/cmd/pasteai@latest
```

Then resolve the path:
```sh
PASTEAI_BIN="$(go env GOPATH)/bin/pasteai"
$PASTEAI_BIN version
```

### Option B: prebuilt binary (no Go required)

```sh
curl -sSL https://raw.githubusercontent.com/pasteai/pasteai/main/install.sh | sh
PASTEAI_BIN="$HOME/.local/bin/pasteai"
$PASTEAI_BIN version
```

If neither Go nor curl is available, tell the user:
> Install Go from https://go.dev/dl/ or download a prebuilt binary from https://github.com/pasteai/pasteai/releases, then re-run /setup.
Then stop.

**Save PASTEAI_BIN** — the full absolute path to the binary. You will use it in Steps 2 and 3.

---

## Step 2 — Start the server (local and remote modes only)

Skip this step if PASTEAI_MODE is `embedded`.

For **local** mode, start a persistent server. Try each option in order:

### Option A: Docker (preferred)

```sh
mkdir -p ~/.pasteai
docker info 2>/dev/null
```

If Docker is available:
```sh
mkdir -p ~/.pasteai
docker run -d --name pasteai --restart unless-stopped \
  -p 8080:8080 -v "${HOME}/.pasteai:/data" \
  ghcr.io/pasteai/pasteai:latest
```

Verify: `docker ps --filter name=pasteai --format "{{.Status}}"`

### Option B: systemd (Linux, no Docker)

```sh
which systemctl && systemctl --user status 2>/dev/null
```

If systemd is available:
```sh
mkdir -p ~/.config/systemd/user
cat > ~/.config/systemd/user/pasteai.service << EOF
[Unit]
Description=PasteAI document server
After=network.target

[Service]
ExecStart=$PASTEAI_BIN serve
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
EOF
systemctl --user daemon-reload
systemctl --user enable pasteai
systemctl --user start pasteai
```

Verify: `systemctl --user status pasteai`

### Option C: launchd (macOS, no Docker)

```sh
mkdir -p ~/Library/LaunchAgents
cat > ~/Library/LaunchAgents/ai.paste.pasteai.plist << EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>ai.paste.pasteai</string>
  <key>ProgramArguments</key>
  <array><string>$PASTEAI_BIN</string><string>serve</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardErrorPath</key><string>/tmp/pasteai.log</string>
</dict>
</plist>
EOF
launchctl load ~/Library/LaunchAgents/ai.paste.pasteai.plist
```

Verify: `launchctl list | grep pasteai`

### Option D: background process (fallback)

```sh
nohup $PASTEAI_BIN serve > ~/.pasteai/server.log 2>&1 &
echo $! > ~/.pasteai/server.pid
```

---

## Step 3 — Register MCP globally in ~/.claude.json

MCP must be registered in `~/.claude.json` (global — available in all projects), not in a project-local `.mcp.json`. Use the **full absolute path** from PASTEAI_BIN.

First ensure the data directory exists:
```sh
mkdir -p ~/.pasteai
```

Then register based on PASTEAI_MODE using the binary directly (no python3 or Node.js required):

**Embedded (automatic — default):**
```sh
$PASTEAI_BIN setup -mode embedded
```

**Local server (manual):**
```sh
$PASTEAI_BIN setup -mode local
```

**Remote:**
```sh
$PASTEAI_BIN setup -mode remote -url $PASTEAI_URL${PASTEAI_API_KEY:+ -api-key $PASTEAI_API_KEY}
```

The `setup` subcommand handles the JSON merge, preserves other MCP servers, and prints numbered next steps. Do not use a bare `"pasteai"` as the command — Claude Code does not inherit the shell PATH.

---

## Step 4 — Done

Tell the user:

> ✓ pasteai is installed at [PASTEAI_BIN]
> ✓ MCP registered globally in ~/.claude.json (mode: [PASTEAI_MODE], available in all Claude Code projects)
>
> **Restart Claude Code** to activate the `publish_document` and `list_documents` tools.
>
> Documents are stored as plain markdown at `~/.pasteai/documents/{id}.md` — readable any time, no server needed.
> The web UI is at http://localhost:8080 (starts automatically on first use in embedded mode).
