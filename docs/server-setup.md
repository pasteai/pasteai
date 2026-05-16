# Persistent Server Setup

Options for running `pasteai serve` so it survives reboots. Pick whichever fits your environment.

## systemd (Linux)

Run this block in your shell — `$PASTEAI` expands to the resolved binary path:

```sh
PASTEAI=$(which pasteai)   # or: $HOME/go/bin/pasteai, $HOME/.local/bin/pasteai
mkdir -p ~/.config/systemd/user
cat > ~/.config/systemd/user/pasteai.service << EOF
[Unit]
Description=PasteAI document server
After=network.target

[Service]
ExecStart=$PASTEAI serve
Restart=on-failure

[Install]
WantedBy=default.target
EOF
systemctl --user daemon-reload
systemctl --user enable --now pasteai
```

Check status: `systemctl --user status pasteai`

## launchd (macOS)

```sh
PASTEAI=$(which pasteai)
cat > ~/Library/LaunchAgents/ai.paste.pasteai.plist << EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>ai.paste.pasteai</string>
  <key>ProgramArguments</key>
  <array><string>$PASTEAI</string><string>serve</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardErrorPath</key><string>/tmp/pasteai.log</string>
</dict>
</plist>
EOF
launchctl load ~/Library/LaunchAgents/ai.paste.pasteai.plist
```

## Docker (without Compose)

```sh
mkdir -p ~/.pasteai
docker run -d --name pasteai --restart unless-stopped \
  -p 8080:8080 -v "${HOME}/.pasteai:/data" \
  ghcr.io/pasteai/pasteai:latest
```

The bind mount puts the database and markdown files directly on your host at `~/.pasteai/`. Documents are readable at `~/.pasteai/documents/{id}.md` even when the container is stopped. Files are owned by UID 10001 (the container's `pasteai` user) — readable by root and accessible on most Linux setups.

## nohup (quick start, no reboots)

```sh
nohup pasteai serve > ~/.pasteai/server.log 2>&1 &
```

## Server flags

| Flag | Default | Description |
|---|---|---|
| `-addr` | `:8080` | Listen address |
| `-db` | `~/.pasteai/documents.db` | Database path |
| `-base-url` | *(from request)* | Base URL for generated links — set for any non-localhost deployment |
| `-api-key` | *(none)* | Require Bearer token on API writes |
