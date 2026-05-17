package integration

import (
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testBinaryPath string

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}

	tmp, err := os.CreateTemp("", "pasteai-integ-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create temp: %v\n", err)
		os.Exit(1)
	}
	tmp.Close()
	binPath := tmp.Name()

	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/pasteai")
	cmd.Dir = findRepoRoot()
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		os.Remove(binPath)
		fmt.Fprintf(os.Stderr, "build binary: %v\n", err)
		os.Exit(1)
	}

	testBinaryPath = binPath
	code := m.Run()
	os.Remove(binPath)
	os.Exit(code)
}

// startServer starts pasteai serve on a random port with an isolated temp db.
// The process is killed when t cleans up.
func startServer(t *testing.T) (addr string, tmpDir string) {
	t.Helper()
	cmd, addr, tmpDir := startServeCmd(t)
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})
	return addr, tmpDir
}

// startServeCmd is like startServer but returns the cmd so callers can kill it themselves.
func startServeCmd(t *testing.T) (cmd *exec.Cmd, addr string, tmpDir string) {
	t.Helper()

	tmpDir = t.TempDir()
	dbPath := filepath.Join(tmpDir, "documents.db")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	addr = fmt.Sprintf("127.0.0.1:%d", port)

	cmd = exec.Command(testBinaryPath, "serve",
		"-addr", fmt.Sprintf(":%d", port),
		"-db", dbPath,
	)
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	if !waitReady("http://" + addr) {
		cmd.Process.Kill()
		t.Fatal("server did not become ready within 5s")
	}
	return cmd, addr, tmpDir
}

func waitReady(baseURL string) bool {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/api/documents")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return true
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func findRepoRoot() string {
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err == nil {
		gomod := strings.TrimSpace(string(out))
		if gomod != "" && gomod != os.DevNull {
			return filepath.Dir(gomod)
		}
	}
	wd, _ := os.Getwd()
	return filepath.Dir(wd)
}
