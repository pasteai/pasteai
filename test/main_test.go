package test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const testVersion = "0.0.0-test"

// testBinaryPath is the host-native pasteai binary built for subprocess tests.
var testBinaryPath string

// installContainer is the running Docker container ID (empty if Docker unavailable).
var installContainer string

// installSrvURL is the URL of the file server, reachable from inside the container.
var installSrvURL string

func TestMain(m *testing.M) {
	flag.Parse()
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	if testing.Short() {
		return m.Run()
	}

	// Build native binary for subprocess tests (TestSetup*).
	tmp, err := os.MkdirTemp("", "pasteai-script-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "mkdirtemp: %v\n", err)
		return 1
	}
	defer os.RemoveAll(tmp)

	binPath := filepath.Join(tmp, "pasteai")
	buildCmd := exec.Command("go", "build", "-o", binPath, "./cmd/pasteai")
	buildCmd.Dir = findRepoRoot()
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "build pasteai binary: %v\n", err)
		return 1
	}
	testBinaryPath = binPath

	if exec.Command("docker", "info").Run() != nil {
		// Docker not available — install tests will skip themselves.
		return m.Run()
	}
	cleanup, err := setupDockerEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "install test setup: %v\n", err)
		return 1
	}
	defer cleanup()
	return m.Run()
}

func setupDockerEnv() (cleanup func(), err error) {
	repoRoot := findRepoRoot()

	binPath, err := buildLinuxBinary(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("build binary: %w", err)
	}

	tarball, err := makeTarball(binPath)
	os.Remove(binPath)
	if err != nil {
		return nil, fmt.Errorf("make tarball: %w", err)
	}

	port, srv := startFileServer(tarball)
	installSrvURL = fmt.Sprintf("http://host.docker.internal:%d", port)

	out, err := exec.Command("docker", "run", "-d",
		"--add-host=host.docker.internal:host-gateway",
		"-v", repoRoot+"/install.sh:/install.sh:ro",
		"ubuntu:24.04",
		"sleep", "3600",
	).Output()
	if err != nil {
		srv.Close()
		return nil, fmt.Errorf("docker run: %w", err)
	}
	id := strings.TrimSpace(string(out))
	installContainer = id

	if err := dockerExec(id, "apt-get", "update", "-qq"); err != nil {
		exec.Command("docker", "rm", "-f", id).Run()
		srv.Close()
		return nil, fmt.Errorf("apt-get update: %w", err)
	}
	if err := dockerExec(id, "apt-get", "install", "-y", "-qq", "curl"); err != nil {
		exec.Command("docker", "rm", "-f", id).Run()
		srv.Close()
		return nil, fmt.Errorf("install curl: %w", err)
	}

	return func() {
		exec.Command("docker", "rm", "-f", id).Run()
		srv.Close()
		installContainer = ""
	}, nil
}

func findRepoRoot() string {
	// go env GOMOD returns the absolute path to go.mod regardless of wd.
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

func buildLinuxBinary(repoRoot string) (string, error) {
	tmp, err := os.CreateTemp("", "pasteai-linux-*")
	if err != nil {
		return "", err
	}
	tmp.Close()

	cmd := exec.Command("go", "build",
		"-ldflags", fmt.Sprintf("-s -w -X main.version=%s", testVersion),
		"-o", tmp.Name(),
		"./cmd/pasteai",
	)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

func makeTarball(binPath string) ([]byte, error) {
	data, err := os.ReadFile(binPath)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	if err := tw.WriteHeader(&tar.Header{
		Name: "pasteai",
		Mode: 0755,
		Size: int64(len(data)),
	}); err != nil {
		return nil, err
	}
	if _, err := tw.Write(data); err != nil {
		return nil, err
	}
	tw.Close()
	gw.Close()
	return buf.Bytes(), nil
}

func startFileServer(tarball []byte) (port int, srv *httptest.Server) {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/latest", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"tag_name": "v" + testVersion})
	})

	tarPath := fmt.Sprintf("/v%s/pasteai_%s_linux_amd64.tar.gz", testVersion, testVersion)
	mux.HandleFunc(tarPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.Write(tarball)
	})

	// Bind on all interfaces so the Docker container can reach us.
	ln, _ := net.Listen("tcp", "0.0.0.0:0")
	port = ln.Addr().(*net.TCPAddr).Port

	srv = httptest.NewUnstartedServer(mux)
	srv.Listener = ln
	srv.Start()
	return port, srv
}

func dockerExec(id string, args ...string) error {
	_, err := exec.Command("docker", append([]string{"exec", id}, args...)...).CombinedOutput()
	return err
}

func dockerExecOutput(id string, args ...string) (string, error) {
	out, err := exec.Command("docker", append([]string{"exec", id}, args...)...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// dockerInstall runs install.sh inside the container with the given HOME and optional mode/URL.
func dockerInstall(home, mode, pasteaiURL string) (string, error) {
	args := []string{"exec",
		"-e", "HOME=" + home,
		"-e", "SHELL=/bin/bash", // needed so install.sh can detect ~/.bashrc
		"-e", "PASTEAI_INSTALL_DIR=" + home + "/.local/bin",
		"-e", "PASTEAI_BASE_URL=" + installSrvURL,
		"-e", "PASTEAI_API_URL=" + installSrvURL + "/api/latest",
	}
	if mode != "" {
		args = append(args, "-e", "PASTEAI_MODE="+mode)
	}
	if pasteaiURL != "" {
		args = append(args, "-e", "PASTEAI_URL="+pasteaiURL)
	}
	args = append(args, installContainer, "sh", "/install.sh")
	out, err := exec.Command("docker", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func requireInstall(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("install test (use -run without -short to run)")
	}
	if installContainer == "" {
		t.Skip("docker daemon not available")
	}
}

func requireBinary(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("setup test (use -run without -short to run)")
	}
	if testBinaryPath == "" {
		t.Skip("test binary not available")
	}
}
