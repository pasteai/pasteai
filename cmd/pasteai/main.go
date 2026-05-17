package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/pasteai/pasteai/internal/api"
	"github.com/pasteai/pasteai/internal/mcp"
	"github.com/pasteai/pasteai/internal/setup"
	"github.com/pasteai/pasteai/internal/store"
)

// version is set at build time via -ldflags "-X main.version=..."
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		runServe(os.Args[2:])
	case "mcp":
		runMCP()
	case "setup":
		if err := setup.Run(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "pasteai setup:", err)
			os.Exit(1)
		}
	case "doctor":
		if err := setup.RunDoctor(os.Args[2:]); err != nil {
			os.Exit(1)
		}
	case "version", "--version", "-v":
		fmt.Println("pasteai", version)
	default:
		printUsage()
		os.Exit(1)
	}
}

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "Address to listen on")
	dbPath := fs.String("db", "", "Path to bbolt database file (default ~/.pasteai/documents.db)")
	baseURL := fs.String("base-url", "", "Base URL for document links (e.g. https://pasteai.io)")
	apiKey := fs.String("api-key", os.Getenv("PASTEAI_API_KEY"), "Require this Bearer token on API writes (optional)")
	fs.Parse(args)

	if *dbPath == "" {
		*dbPath = defaultDBPath()
	}
	if err := os.MkdirAll(filepath.Dir(*dbPath), 0700); err != nil {
		fmt.Fprintf(os.Stderr, "pasteai: cannot create data directory %s: %v\n", filepath.Dir(*dbPath), err)
		os.Exit(1)
	}

	logger := log.New(os.Stderr, "[pasteai] ", log.LstdFlags)

	s, err := store.NewBolt(*dbPath)
	if err != nil {
		logger.Fatalf("failed to open database at %s: %v", *dbPath, err)
	}
	defer s.Close()

	cfg := api.Config{
		Addr:    *addr,
		BaseURL: *baseURL,
		Logger:  logger,
	}
	if *apiKey != "" {
		cfg.AuthProvider = api.NewStaticKeyAuth(map[string]string{*apiKey: "owner"})
	} else {
		logger.Printf("warning: no -api-key set; all API writes including DELETE are open to any caller that can reach %s", *addr)
	}
	srv := api.NewServer(s, cfg)

	logger.Printf("pasteai v%s listening on %s", version, *addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Fatal(err)
	}
}

func runMCP() {
	s := mcp.New()
	if err := s.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "mcp error: %v\n", err)
		os.Exit(1)
	}
}

func defaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "pasteai.db"
	}
	return filepath.Join(home, ".pasteai", "documents.db")
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `PasteAI — AI document sharing service

Usage:
  pasteai setup [-mode automatic|manual|remote] [-url URL] [-api-key KEY]
                  Configure MCP in ~/.claude.json
  pasteai doctor  Diagnose common setup problems
  pasteai serve [flags]   Start the web server
  pasteai mcp             Start the MCP server (reads PASTEAI_URL env var)
  pasteai version         Print version

Serve flags:
  -addr string      Address to listen on (default ":8080")
  -db string        Path to database file (default ~/.pasteai/documents.db)
  -base-url string  Base URL for links, e.g. https://pasteai.io
  -api-key string   Require this Bearer token on API writes (optional; set via PASTEAI_API_KEY)

MCP environment variables:
  PASTEAI_URL       URL of the pasteai server; if unset, an embedded server starts automatically
  PASTEAI_API_KEY   API key for authenticated access (optional)
`)
}
