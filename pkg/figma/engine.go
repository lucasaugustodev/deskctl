// Package figma provides automatic Figma daemon management.
// Handles: detect Figma, check daemon, start daemon, execute commands.
package figma

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const daemonPort = 3456

type Engine struct {
	token string
}

type daemonResp struct {
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
	Status string `json:"status,omitempty"`
	CDP    bool   `json:"cdp,omitempty"`
}

// Start ensures Figma + daemon are running, returns ready engine.
func Start() (*Engine, error) {
	// 1. Check if Figma is running
	if !isFigmaRunning() {
		return nil, fmt.Errorf("Figma is not running. Open Figma Desktop and a design file first")
	}

	// 2. Read or wait for token
	token := readToken()

	// 3. Check if daemon is alive
	if token != "" && isDaemonAlive(token) {
		// Check if connected to a design file
		e := &Engine{token: token}
		if e.isConnected() {
			return e, nil
		}
		// Daemon alive but not connected — might need design file
	}

	// 4. Start daemon if not running
	fmt.Fprintf(os.Stderr, "[figma] starting daemon...\n")
	if err := startDaemon(); err != nil {
		return nil, fmt.Errorf("failed to start daemon: %w", err)
	}

	// Re-read token (daemon generates a new one)
	time.Sleep(2 * time.Second)
	token = readToken()
	if token == "" {
		return nil, fmt.Errorf("daemon started but no token found")
	}

	// 5. Wait for daemon to connect (up to 15s)
	e := &Engine{token: token}
	for i := 0; i < 15; i++ {
		if e.isConnected() {
			fmt.Fprintf(os.Stderr, "[figma] connected\n")
			return e, nil
		}
		time.Sleep(1 * time.Second)
	}

	return nil, fmt.Errorf("daemon running but cannot find Figma execution context. Make sure a design file is open in Figma (not the home/feed page)")
}

// Eval executes Figma Plugin API code via the daemon.
func (e *Engine) Eval(code string) (string, error) {
	body := map[string]string{"action": "eval", "code": code}
	return e.post(body)
}

func (e *Engine) post(body any) (string, error) {
	data, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", fmt.Sprintf("http://localhost:%d/exec", daemonPort), strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Daemon-Token", e.token)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("daemon request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	var r daemonResp
	json.Unmarshal(respBody, &r)
	if r.Error != "" {
		return "", fmt.Errorf("%s", r.Error)
	}
	return r.Result, nil
}

func (e *Engine) isConnected() bool {
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d/health", daemonPort), nil)
	req.Header.Set("X-Daemon-Token", e.token)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var r daemonResp
	json.Unmarshal(func() []byte { b, _ := io.ReadAll(resp.Body); return b }(), &r)

	if r.Status != "ok" || !r.CDP {
		return false
	}

	// Also check if figma context is available
	result, err := e.Eval("typeof figma !== 'undefined' ? 'yes' : 'no'")
	return err == nil && strings.Contains(result, "yes")
}

// ── Helpers ──

func isFigmaRunning() bool {
	out, _ := exec.Command("tasklist").Output()
	return strings.Contains(strings.ToLower(string(out)), "figma.exe")
}

func isDaemonAlive(token string) bool {
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d/health", daemonPort), nil)
	req.Header.Set("X-Daemon-Token", token)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

func readToken() string {
	home, _ := os.UserHomeDir()
	tokenPath := filepath.Join(home, ".figma-ds-cli", ".daemon-token")
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func startDaemon() error {
	// Find daemon.js from figma-cli
	daemonScript := findDaemonScript()
	if daemonScript == "" {
		return fmt.Errorf("figma-cli not found. Install: git clone https://github.com/silships/figma-cli.git && cd figma-cli && npm install")
	}

	// Kill old daemon if port is in use
	if runtime.GOOS == "windows" {
		exec.Command("cmd", "/C", "for /f \"tokens=5\" %a in ('netstat -ano ^| findstr :3456 ^| findstr LISTENING') do taskkill /F /PID %a").Run()
	}
	time.Sleep(1 * time.Second)

	// Generate token
	home, _ := os.UserHomeDir()
	tokenDir := filepath.Join(home, ".figma-ds-cli")
	os.MkdirAll(tokenDir, 0700)

	// Start daemon.js as detached process
	cmd := exec.Command("node", daemonScript)
	cmd.Env = append(os.Environ(), "DAEMON_PORT=3456", "DAEMON_MODE=auto")
	cmd.Dir = filepath.Dir(daemonScript)
	cmd.Stdout = nil
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	// Save PID
	pidPath := filepath.Join(home, ".figma-cli-daemon.pid")
	os.WriteFile(pidPath, []byte(fmt.Sprintf("%d", cmd.Process.Pid)), 0644)

	// Don't wait for the process
	go cmd.Wait()

	return nil
}

func findDaemonScript() string {
	candidates := []string{
		filepath.Join("..", "figma-cli", "src", "daemon.js"),
		filepath.Join(".", "figma-cli", "src", "daemon.js"),
	}

	// Check relative to executable
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates,
			filepath.Join(filepath.Dir(exe), "..", "figma-cli", "src", "daemon.js"),
			filepath.Join(filepath.Dir(exe), "figma-cli", "src", "daemon.js"),
		)
	}

	// Common locations
	home, _ := os.UserHomeDir()
	candidates = append(candidates,
		filepath.Join(home, "Documents", "GitHub", "figma-cli", "src", "daemon.js"),
		`C:\Users\PC\Documents\GitHub\figma-cli\src\daemon.js`,
	)

	for _, p := range candidates {
		if abs, err := filepath.Abs(p); err == nil {
			if _, err := os.Stat(abs); err == nil {
				return abs
			}
		}
	}
	return ""
}
