package connector

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type AppInfo struct {
	Type    string `json:"type"`    // "electron", "webview2", "native"
	Exe     string `json:"exe"`
	CDPPort int    `json:"cdp_port,omitempty"`
	Patched bool   `json:"patched,omitempty"`
}

// Detect discovers what type of app is running and finds/enables CDP.
func Detect(appName string) (*AppInfo, error) {
	// Check WebView2 first (PWA apps)
	if port := findWebView2Port(appName); port > 0 {
		return &AppInfo{Type: "webview2", CDPPort: port}, nil
	}

	// Check Electron (look for app.asar near exe)
	exe, cmdline := findProcess(appName)
	if exe == "" {
		return nil, fmt.Errorf("app not found: %s", appName)
	}

	if isElectron(exe) {
		// Check if already has debug port
		if port := extractDebugPort(cmdline); port > 0 && isPortAlive(port) {
			return &AppInfo{Type: "electron", Exe: exe, CDPPort: port}, nil
		}
		return &AppInfo{Type: "electron", Exe: exe}, nil
	}

	return &AppInfo{Type: "native", Exe: exe}, nil
}

// PatchElectron patches an Electron app's app.asar to allow --remote-debugging-port.
func PatchElectron(exe string) error {
	dir := filepath.Dir(exe)
	asar := filepath.Join(dir, "resources", "app.asar")

	data, err := os.ReadFile(asar)
	if err != nil {
		return fmt.Errorf("read asar: %w", err)
	}

	old := []byte(`removeSwitch("remote-debugging-port")`)
	patched := []byte(`removeSwitch("remote-debugXing-port")`)

	if !contains(data, old) {
		if contains(data, patched) {
			return nil // already patched
		}
		return fmt.Errorf("patch target not found in asar")
	}

	// Backup
	os.WriteFile(asar+".bak", data, 0644)

	// Patch
	data = replace(data, old, patched)
	return os.WriteFile(asar, data, 0644)
}

func findWebView2Port(appName string) int {
	out, _ := exec.Command("wmic", "process", "where", "name='msedgewebview2.exe'",
		"get", "CommandLine").Output()
	lower := strings.ToLower(appName)
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(strings.ToLower(line), lower) {
			re := regexp.MustCompile(`--remote-debugging-port=(\d+)`)
			if m := re.FindStringSubmatch(line); len(m) > 1 {
				var port int
				fmt.Sscanf(m[1], "%d", &port)
				if isPortAlive(port) {
					return port
				}
			}
		}
	}
	return 0
}

func findProcess(appName string) (exe, cmdline string) {
	out, _ := exec.Command("wmic", "process", "where",
		fmt.Sprintf("name like '%%%s%%'", appName),
		"get", "ExecutablePath,CommandLine").Output()
	lines := strings.Split(string(out), "\n")
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		re := regexp.MustCompile(`([A-Za-z]:\\[^\s]*\.exe)`)
		if m := re.FindString(line); m != "" {
			return m, line
		}
	}
	return "", ""
}

func isElectron(exe string) bool {
	dir := filepath.Dir(exe)
	asarPath := filepath.Join(dir, "resources", "app.asar")
	if _, err := os.Stat(asarPath); err == nil {
		return true
	}
	return false
}

func extractDebugPort(cmdline string) int {
	re := regexp.MustCompile(`--remote-debugging-port=(\d+)`)
	if m := re.FindStringSubmatch(cmdline); len(m) > 1 {
		var port int
		fmt.Sscanf(m[1], "%d", &port)
		return port
	}
	return 0
}

func isPortAlive(port int) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://localhost:%d/json/version", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var info map[string]any
	json.NewDecoder(resp.Body).Decode(&info)
	_, ok := info["Browser"]
	return ok
}

func contains(data, sub []byte) bool {
	for i := 0; i <= len(data)-len(sub); i++ {
		match := true
		for j := range sub {
			if data[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func replace(data, old, new_ []byte) []byte {
	for i := 0; i <= len(data)-len(old); i++ {
		match := true
		for j := range old {
			if data[i+j] != old[j] {
				match = false
				break
			}
		}
		if match {
			copy(data[i:], new_)
			return data
		}
	}
	return data
}
